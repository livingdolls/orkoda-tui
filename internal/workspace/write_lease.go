package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AcquireWrite acquires exclusive mutation access to a READY workspace.
// Expired WRITE_LOCKED leases may be taken over by a new worker.
func (r *Repository) AcquireWrite(ctx context.Context, workspaceID, owner string, ttl time.Duration) (Lease, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	owner = strings.TrimSpace(owner)
	if workspaceID == "" || owner == "" || ttl <= 0 {
		return Lease{}, fmt.Errorf("%w: workspace ID, lease owner, and positive TTL are required", ErrInvalidWorkspace)
	}

	now := r.now().UTC()
	expiresAt := now.Add(ttl)
	token := newID()
	row := r.db.QueryRowContext(ctx, `
		UPDATE workspaces
		SET lease_owner = ?, lease_token = ?, lease_expires_at = ?,
			status = 'WRITE_LOCKED', updated_at = ?
		WHERE id = ?
			AND (
				(status = 'READY' AND lease_token IS NULL)
				OR (status = 'WRITE_LOCKED' AND lease_expires_at <= ?)
				OR (status = 'WRITE_LOCKED' AND lease_owner = ?)
			)
		RETURNING `+workspaceColumns+`
	`, owner, token, expiresAt.UnixMilli(), now.UnixMilli(), workspaceID, now.UnixMilli(), owner)
	item, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := r.getByID(ctx, workspaceID); errors.Is(getErr, ErrNotFound) {
			return Lease{}, ErrNotFound
		}
		return Lease{}, ErrLeaseUnavailable
	}
	if err != nil {
		return Lease{}, fmt.Errorf("acquire workspace write lease: %w", err)
	}
	return Lease{Workspace: item, Token: token}, nil
}

// ReleaseWrite releases a mutation lease and persists the observed workspace state.
func (r *Repository) ReleaseWrite(
	ctx context.Context,
	workspaceID, token, headSHA string,
	dirty bool,
) (Workspace, error) {
	headSHA = strings.TrimSpace(headSHA)
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(token) == "" || headSHA == "" {
		return Workspace{}, fmt.Errorf("%w: workspace ID, token, and HEAD are required", ErrInvalidWorkspace)
	}
	now := r.now().UTC()
	row := r.db.QueryRowContext(ctx, `
		UPDATE workspaces
		SET status = 'READY', head_sha = ?, dirty = ?,
			lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL,
			failure_message = NULL, updated_at = ?
		WHERE id = ? AND status = 'WRITE_LOCKED'
			AND lease_token = ? AND lease_expires_at > ?
		RETURNING `+workspaceColumns+`
	`, headSHA, boolInteger(dirty), now.UnixMilli(), workspaceID, token, now.UnixMilli())
	item, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrLeaseLost
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("release workspace write lease: %w", err)
	}
	return item, nil
}
