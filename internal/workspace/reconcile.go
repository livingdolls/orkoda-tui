package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReconcileReport describes filesystem/database drift found below the managed
// workspace root. Orphan directories are moved into a recoverable quarantine;
// they are never recursively deleted by startup reconciliation.
type ReconcileReport struct {
	Orphaned []string `json:"orphaned"`
	Missing  []string `json:"missing"`
}

// CleanupReport describes an explicit retention cleanup. Archived workspace
// directories are moved to a separate recoverable directory before orphan
// quarantine entries are removed.
type CleanupReport struct {
	Archived       []string `json:"archived"`
	RemovedOrphans []string `json:"removed_orphans"`
	Skipped        []string `json:"skipped"`
}

func (r *Repository) ReconcileOrphans(ctx context.Context) (ReconcileReport, error) {
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return ReconcileReport{}, fmt.Errorf("list managed workspace root: %w", err)
	}
	known := make(map[string]string)
	rows, err := r.db.QueryContext(ctx, `SELECT id, path FROM workspaces`)
	if err != nil {
		return ReconcileReport{}, fmt.Errorf("list persisted workspaces: %w", err)
	}
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			rows.Close()
			return ReconcileReport{}, fmt.Errorf("scan persisted workspace: %w", err)
		}
		known[filepath.Clean(path)] = id
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ReconcileReport{}, fmt.Errorf("iterate persisted workspaces: %w", err)
	}
	rows.Close()

	report := ReconcileReport{Orphaned: []string{}, Missing: []string{}}
	for path := range known {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			report.Missing = append(report.Missing, known[path])
			if err := r.markMissing(ctx, known[path]); err != nil {
				return report, err
			}
		} else if err != nil {
			return report, fmt.Errorf("inspect workspace %s: %w", known[path], err)
		}
	}

	quarantine := filepath.Join(r.root, ".orphans")
	if err := os.MkdirAll(quarantine, 0o700); err != nil {
		return report, fmt.Errorf("create orphan quarantine: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == ".orphans" {
			continue
		}
		path := filepath.Join(r.root, entry.Name())
		if _, ok := known[filepath.Clean(path)]; ok {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			return report, fmt.Errorf("inspect orphan candidate %s: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return report, fmt.Errorf("refusing symlink in workspace root: %s", path)
		}
		if !info.IsDir() {
			continue
		}
		destination := filepath.Join(quarantine, fmt.Sprintf("%d-%s", time.Now().UTC().UnixNano(), entry.Name()))
		if err := os.Rename(path, destination); err != nil {
			return report, fmt.Errorf("quarantine orphan workspace %s: %w", path, err)
		}
		report.Orphaned = append(report.Orphaned, path)
	}
	return report, nil
}

// Archive marks a workspace as no longer active. An unexpired preparation or
// write lease must be released first; archiving never silently steals a live
// lease. The filesystem path remains unchanged until Cleanup is explicitly
// requested so audit and recovery can still find it.
func (r *Repository) Archive(ctx context.Context, workspaceID string) (Workspace, error) {
	now := r.now().UTC()
	row := r.db.QueryRowContext(ctx, `
		UPDATE workspaces
		SET status = 'ARCHIVED', lease_owner = NULL, lease_token = NULL,
			lease_expires_at = NULL, updated_at = ?
		WHERE id = ?
			AND (lease_token IS NULL OR lease_expires_at IS NULL OR lease_expires_at <= ?)
		RETURNING `+workspaceColumns+`
	`, now.UnixMilli(), strings.TrimSpace(workspaceID), now.UnixMilli())
	item, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := r.getByID(ctx, strings.TrimSpace(workspaceID)); errors.Is(getErr, ErrNotFound) {
			return Workspace{}, ErrNotFound
		}
		return Workspace{}, ErrLeaseUnavailable
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("archive workspace: %w", err)
	}
	return item, nil
}

// Cleanup applies an explicit retention boundary. It only touches archived
// workspaces and entries already quarantined below .orphans. Active workspace
// directories are never removed, and archived directories are moved to
// .archive rather than deleted so the operation remains recoverable.
func (r *Repository) Cleanup(ctx context.Context, before time.Time) (CleanupReport, error) {
	before = before.UTC()
	report := CleanupReport{
		Archived:       []string{},
		RemovedOrphans: []string{},
		Skipped:        []string{},
	}
	archiveRoot := filepath.Join(r.root, ".archive")
	if err := os.MkdirAll(archiveRoot, 0o700); err != nil {
		return report, fmt.Errorf("create workspace archive: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, path
		FROM workspaces
		WHERE status = 'ARCHIVED' AND updated_at < ?
			AND (lease_token IS NULL OR lease_expires_at IS NULL OR lease_expires_at <= ?)
		ORDER BY updated_at ASC
	`, before.UnixMilli(), r.now().UTC().UnixMilli())
	if err != nil {
		return report, fmt.Errorf("list archived workspaces: %w", err)
	}
	defer rows.Close()

	type archivedWorkspace struct {
		id   string
		path string
	}
	items := make([]archivedWorkspace, 0)
	for rows.Next() {
		var item archivedWorkspace
		if err := rows.Scan(&item.id, &item.path); err != nil {
			return report, fmt.Errorf("scan archived workspace: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("iterate archived workspaces: %w", err)
	}

	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if err := validateManagedChild(r.root, item.path); err != nil {
			return report, fmt.Errorf("validate archived workspace %s: %w", item.id, err)
		}
		info, err := os.Lstat(item.path)
		if os.IsNotExist(err) {
			report.Skipped = append(report.Skipped, item.path)
			continue
		}
		if err != nil {
			return report, fmt.Errorf("inspect archived workspace %s: %w", item.id, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return report, fmt.Errorf("refusing unsafe archived workspace path: %s", item.path)
		}
		destination := filepath.Join(archiveRoot, fmt.Sprintf("%d-%s", r.now().UTC().UnixNano(), item.id))
		if err := os.Rename(item.path, destination); err != nil {
			return report, fmt.Errorf("archive workspace %s: %w", item.id, err)
		}
		report.Archived = append(report.Archived, item.path)
	}

	quarantine := filepath.Join(r.root, ".orphans")
	entries, err := os.ReadDir(quarantine)
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return report, fmt.Errorf("list orphan quarantine: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		path := filepath.Join(quarantine, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return report, fmt.Errorf("inspect quarantined orphan %s: %w", entry.Name(), err)
		}
		if info.ModTime().After(before) {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			report.Skipped = append(report.Skipped, path)
			continue
		}
		if err := validateManagedChild(quarantine, path); err != nil {
			return report, err
		}
		if err := os.RemoveAll(path); err != nil {
			return report, fmt.Errorf("remove quarantined orphan %s: %w", path, err)
		}
		report.RemovedOrphans = append(report.RemovedOrphans, path)
	}
	return report, nil
}

func validateManagedChild(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path is outside managed root: %s", path)
	}
	return nil
}

func (r *Repository) markMissing(ctx context.Context, workspaceID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE workspaces
		SET status = CASE WHEN status IN ('ARCHIVED', 'FAILED') THEN status ELSE 'FAILED' END,
			failure_message = CASE WHEN status IN ('ARCHIVED', 'FAILED') THEN failure_message ELSE ? END,
			lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL, updated_at = ?
		WHERE id = ?
	`, "workspace path is missing from managed root", time.Now().UTC().UnixMilli(), strings.TrimSpace(workspaceID))
	if err != nil {
		return fmt.Errorf("mark missing workspace %s: %w", workspaceID, err)
	}
	return nil
}

var _ interface {
	ReconcileOrphans(context.Context) (ReconcileReport, error)
} = (*Repository)(nil)
