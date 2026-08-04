package diagnostics

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/artifact"
)

type Snapshot struct {
	Service     string          `json:"service"`
	Status      string          `json:"status"`
	Protocol    string          `json:"protocol_version"`
	Database    DatabaseStatus  `json:"database"`
	Queue       QueueStatus     `json:"queue"`
	Activity    ActivityStatus  `json:"activity"`
	Workspaces  WorkspaceStatus `json:"workspaces"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type DatabaseStatus struct {
	Integrity string `json:"integrity"`
	Schema    int    `json:"schema_version"`
}

type QueueStatus struct {
	Queued  int `json:"queued"`
	Running int `json:"running"`
	Dead    int `json:"dead"`
}

type ActivityStatus struct {
	Events       int   `json:"events"`
	LastSequence int64 `json:"last_sequence"`
}

type WorkspaceStatus struct {
	Total         int `json:"total"`
	ActiveLeases  int `json:"active_leases"`
	ExpiredLeases int `json:"expired_leases"`
}

type Reader interface {
	Read(context.Context) (Snapshot, error)
	Bundle(context.Context) (string, error)
}

type Service struct {
	db        *sql.DB
	artifacts artifact.Store
}

func NewService(db *sql.DB, artifacts artifact.Store) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	return &Service{db: db, artifacts: artifacts}, nil
}

func (s *Service) Read(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	snapshot.Service = "orkoda-local-daemon"
	snapshot.Protocol = "v1"
	snapshot.GeneratedAt = time.Now().UTC()
	if err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&snapshot.Database.Integrity); err != nil {
		return Snapshot{}, fmt.Errorf("read SQLite integrity: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&snapshot.Database.Schema); err != nil {
		return Snapshot{}, fmt.Errorf("read SQLite schema version: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status = 'QUEUED'`).Scan(&snapshot.Queue.Queued); err != nil {
		return Snapshot{}, fmt.Errorf("count queued jobs: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status = 'RUNNING'`).Scan(&snapshot.Queue.Running); err != nil {
		return Snapshot{}, fmt.Errorf("count running jobs: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status = 'DEAD'`).Scan(&snapshot.Queue.Dead); err != nil {
		return Snapshot{}, fmt.Errorf("count dead jobs: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(sequence), 0) FROM activity_events`).Scan(&snapshot.Activity.Events, &snapshot.Activity.LastSequence); err != nil {
		return Snapshot{}, fmt.Errorf("count activity events: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN lease_token IS NOT NULL THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN lease_token IS NOT NULL AND lease_expires_at <= ? THEN 1 ELSE 0 END), 0) FROM workspaces`, time.Now().UTC().UnixMilli()).Scan(&snapshot.Workspaces.Total, &snapshot.Workspaces.ActiveLeases, &snapshot.Workspaces.ExpiredLeases); err != nil {
		return Snapshot{}, fmt.Errorf("count workspaces: %w", err)
	}
	snapshot.Status = "ready"
	if snapshot.Database.Integrity != "ok" || snapshot.Queue.Dead > 0 {
		snapshot.Status = "degraded"
	}
	return snapshot, nil
}

func (s *Service) Bundle(ctx context.Context) (string, error) {
	snapshot, err := s.Read(ctx)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("marshal diagnostics bundle: %w", err)
	}
	if s.artifacts == nil {
		return "", fmt.Errorf("artifact storage is unavailable")
	}
	key := fmt.Sprintf("diagnostics/%s.json", snapshot.GeneratedAt.Format("20060102T150405.000000000Z"))
	if err := s.artifacts.Save(ctx, key, bytes.NewReader(payload)); err != nil {
		return "", fmt.Errorf("save diagnostics bundle: %w", err)
	}
	return key, nil
}
