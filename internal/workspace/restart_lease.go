package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AcquireRestart reserves a failed workflow workspace for destructive reset.
// Active mutation leases are never stolen, even when they belong to the same daemon.
func (r *Repository) AcquireRestart(
	ctx context.Context,
	workspaceID string,
	owner string,
	ttl time.Duration,
) (Lease, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	owner = strings.TrimSpace(owner)
	if workspaceID == "" || owner == "" || ttl <= 0 {
		return Lease{}, fmt.Errorf(
			"%w: workspace ID, lease owner, and positive TTL are required",
			ErrInvalidWorkspace,
		)
	}

	now := r.now().UTC()
	expiresAt := now.Add(ttl)
	token := newID()
	row := r.db.QueryRowContext(ctx, `
		UPDATE workspaces
		SET lease_owner = ?, lease_token = ?, lease_expires_at = ?,
			status = 'PREPARING', head_sha = '', dirty = 0,
			failure_message = NULL, updated_at = ?
		WHERE id = ?
			AND (
				(
					status IN ('REQUESTED', 'PREPARING', 'READY', 'FAILED')
					AND (lease_token IS NULL OR lease_expires_at <= ?)
				)
				OR (status = 'WRITE_LOCKED' AND lease_expires_at <= ?)
			)
		RETURNING `+workspaceColumns+`
	`, owner, token, expiresAt.UnixMilli(), now.UnixMilli(), workspaceID,
		now.UnixMilli(), now.UnixMilli())
	item, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := r.getByID(ctx, workspaceID); errors.Is(getErr, ErrNotFound) {
			return Lease{}, ErrNotFound
		}
		return Lease{}, ErrLeaseUnavailable
	}
	if err != nil {
		return Lease{}, fmt.Errorf("acquire workspace restart lease: %w", err)
	}
	return Lease{Workspace: item, Token: token}, nil
}
