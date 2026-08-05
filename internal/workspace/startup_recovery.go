package workspace

import (
	"context"
	"fmt"
	"time"
)

// RecoverDaemonLeases releases mutation leases owned by an earlier local
// daemon instance. Manual client leases are intentionally preserved.
func (r *Repository) RecoverDaemonLeases(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		UPDATE workspaces
		SET status = CASE WHEN status = 'WRITE_LOCKED' THEN 'READY' ELSE status END,
			lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL,
			updated_at = ?
		WHERE lease_token IS NOT NULL
			AND lease_owner LIKE 'local-daemon-%'
		RETURNING id
	`, now.UTC().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("recover interrupted daemon workspace leases: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan recovered daemon workspace lease: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovered daemon workspace leases: %w", err)
	}
	return ids, nil
}
