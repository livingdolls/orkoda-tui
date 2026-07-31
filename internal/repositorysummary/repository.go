package repositorysummary

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

var (
	ErrNotFound           = errors.New("repository summary not found")
	ErrRepositoryNotFound = errors.New("repository not found")
)

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Summary struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	ProjectID    string    `json:"project_id"`
	HeadSHA      string    `json:"head_sha"`
	Dirty        bool      `json:"dirty"`
	Snapshot     Snapshot  `json:"summary"`
	CreatedAt    time.Time `json:"created_at"`
}

type repositoryState struct {
	ID        string
	ProjectID string
	LocalPath string
	HeadSHA   string
	Dirty     bool
}

type Repository struct {
	db       *sql.DB
	scanner  Scanner
	recorder EventRecorder
}

func NewRepository(db *sql.DB, scanner Scanner, recorder EventRecorder) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if scanner == nil {
		return nil, fmt.Errorf("repository scanner is required")
	}
	return &Repository{db: db, scanner: scanner, recorder: recorder}, nil
}

func (r *Repository) Generate(ctx context.Context, repositoryID string) (Summary, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return Summary{}, fmt.Errorf("repository ID is required")
	}

	state, err := r.repositoryState(ctx, repositoryID)
	if err != nil {
		return Summary{}, err
	}
	if existing, err := r.getByHead(ctx, state, state.HeadSHA); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Summary{}, err
	}

	startedAt := time.Now().UTC()
	r.record(ctx, "repository.summary_started", map[string]any{
		"project_id": state.ProjectID, "repository_id": state.ID, "head_sha": state.HeadSHA,
	}, startedAt)

	snapshot, err := r.scanner.Scan(ctx, state.LocalPath, state.HeadSHA)
	if err != nil {
		r.recordFailure(ctx, state, err)
		return Summary{}, fmt.Errorf("scan repository: %w", err)
	}
	payloadJSON, err := json.Marshal(snapshot)
	if err != nil {
		r.recordFailure(ctx, state, err)
		return Summary{}, fmt.Errorf("marshal repository summary: %w", err)
	}

	createdAt := time.Now().UTC()
	if _, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO repository_summaries (
			id, repository_id, head_sha, summary_json, created_at
		) VALUES (?, ?, ?, ?, ?)
	`, newID(), state.ID, state.HeadSHA, string(payloadJSON), createdAt.UnixMilli()); err != nil {
		r.recordFailure(ctx, state, err)
		return Summary{}, fmt.Errorf("store repository summary: %w", err)
	}

	summary, err := r.getByHead(ctx, state, state.HeadSHA)
	if err != nil {
		r.recordFailure(ctx, state, err)
		return Summary{}, err
	}
	r.record(ctx, "repository.summary_completed", map[string]any{
		"project_id": state.ProjectID,
		"repository_id": state.ID,
		"summary_id": summary.ID,
		"head_sha": state.HeadSHA,
		"file_count": summary.Snapshot.FileCount,
		"truncated": summary.Snapshot.Truncated,
	}, summary.CreatedAt)
	return summary, nil
}

func (r *Repository) Current(ctx context.Context, repositoryID string) (Summary, error) {
	state, err := r.repositoryState(ctx, strings.TrimSpace(repositoryID))
	if err != nil {
		return Summary{}, err
	}
	return r.getByHead(ctx, state, state.HeadSHA)
}

func (r *Repository) repositoryState(ctx context.Context, repositoryID string) (repositoryState, error) {
	var state repositoryState
	var dirty int
	err := r.db.QueryRowContext(ctx, `
		SELECT id, project_id, local_path, head_sha, dirty
		FROM repositories WHERE id = ?
	`, repositoryID).Scan(&state.ID, &state.ProjectID, &state.LocalPath, &state.HeadSHA, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return repositoryState{}, ErrRepositoryNotFound
	}
	if err != nil {
		return repositoryState{}, fmt.Errorf("read repository: %w", err)
	}
	state.Dirty = dirty == 1
	return state, nil
}

func (r *Repository) getByHead(ctx context.Context, state repositoryState, headSHA string) (Summary, error) {
	var summary Summary
	var snapshotJSON string
	var createdAt int64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, repository_id, head_sha, summary_json, created_at
		FROM repository_summaries
		WHERE repository_id = ? AND head_sha = ?
	`, state.ID, headSHA).Scan(
		&summary.ID,
		&summary.RepositoryID,
		&summary.HeadSHA,
		&snapshotJSON,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, fmt.Errorf("read repository summary: %w", err)
	}
	if err := json.Unmarshal([]byte(snapshotJSON), &summary.Snapshot); err != nil {
		return Summary{}, fmt.Errorf("decode repository summary: %w", err)
	}
	summary.ProjectID = state.ProjectID
	summary.Dirty = state.Dirty
	summary.CreatedAt = time.UnixMilli(createdAt).UTC()
	return summary, nil
}

func (r *Repository) recordFailure(ctx context.Context, state repositoryState, cause error) {
	r.record(ctx, "repository.summary_failed", map[string]any{
		"project_id": state.ProjectID,
		"repository_id": state.ID,
		"head_sha": state.HeadSHA,
		"error": cause.Error(),
	}, time.Now().UTC())
}

func (r *Repository) record(ctx context.Context, eventType string, payload any, createdAt time.Time) {
	if r.recorder == nil {
		return
	}
	if err := r.recorder.Record(ctx, "", eventType, payload, createdAt); err != nil {
		slog.Warn("record repository summary activity", "event_type", eventType, "error", err)
	}
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate repository summary ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
