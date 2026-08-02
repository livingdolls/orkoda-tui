package workspace

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("workspace not found")
	ErrInvalidWorkspace  = errors.New("invalid workspace")
	ErrLeaseUnavailable  = errors.New("workspace lease is unavailable")
	ErrLeaseLost         = errors.New("workspace lease is no longer owned")
	ErrImmutableConflict = errors.New("workspace immutable fields conflict")
)

type Status string

const (
	StatusRequested   Status = "REQUESTED"
	StatusPreparing   Status = "PREPARING"
	StatusReady       Status = "READY"
	StatusWriteLocked Status = "WRITE_LOCKED"
	StatusArchived    Status = "ARCHIVED"
	StatusFailed      Status = "FAILED"
)

type Workspace struct {
	ID             string     `json:"id"`
	WorkflowJobID  string     `json:"workflow_job_id"`
	ProjectID      string     `json:"project_id"`
	RepositoryID   string     `json:"repository_id"`
	Path           string     `json:"path"`
	BaseCommitSHA  string     `json:"base_commit_sha"`
	HeadSHA        string     `json:"head_sha,omitempty"`
	Status         Status     `json:"status"`
	Dirty          bool       `json:"dirty"`
	LeaseOwner     string     `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	FailureMessage string     `json:"failure_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type SourceRepository struct {
	ID        string
	LocalPath string
}

type Lease struct {
	Workspace Workspace
	Token     string
}

type Repository struct {
	db   *sql.DB
	root string
	now  func() time.Time
}

func NewRepository(db *sql.DB, root string) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("workspace root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	return &Repository{db: db, root: filepath.Clean(absolute), now: time.Now}, nil
}

func (r *Repository) Root() string {
	return r.root
}

func (r *Repository) EnsureForWorkflow(ctx context.Context, workflowJobID string) (Workspace, SourceRepository, error) {
	workflowJobID = strings.TrimSpace(workflowJobID)
	if err := validateIdentifier(workflowJobID); err != nil {
		return Workspace{}, SourceRepository{}, err
	}

	var projectID, repositoryID, baseCommitSHA, localPath string
	err := r.db.QueryRowContext(ctx, `
		SELECT w.project_id, w.repository_id, w.base_commit_sha, r.local_path
		FROM workflow_jobs w
		JOIN repositories r ON r.id = w.repository_id
		WHERE w.id = ?
	`, workflowJobID).Scan(&projectID, &repositoryID, &baseCommitSHA, &localPath)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, SourceRepository{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, SourceRepository{}, fmt.Errorf("resolve workspace source: %w", err)
	}

	workspacePath := filepath.Join(r.root, workflowJobID)
	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, SourceRepository{}, fmt.Errorf("begin workspace ensure: %w", err)
	}
	defer tx.Rollback()

	existing, err := loadByWorkflow(ctx, tx, workflowJobID)
	if err == nil {
		if existing.ProjectID != projectID || existing.RepositoryID != repositoryID ||
			existing.BaseCommitSHA != baseCommitSHA || filepath.Clean(existing.Path) != workspacePath {
			return Workspace{}, SourceRepository{}, ErrImmutableConflict
		}
		return existing, SourceRepository{ID: repositoryID, LocalPath: localPath}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Workspace{}, SourceRepository{}, err
	}

	created := Workspace{
		ID:            newID(),
		WorkflowJobID: workflowJobID,
		ProjectID:     projectID,
		RepositoryID:  repositoryID,
		Path:          workspacePath,
		BaseCommitSHA: baseCommitSHA,
		Status:        StatusRequested,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO workspaces (
			id, workflow_job_id, project_id, repository_id, path,
			base_commit_sha, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, created.ID, created.WorkflowJobID, created.ProjectID, created.RepositoryID,
		created.Path, created.BaseCommitSHA, created.Status,
		now.UnixMilli(), now.UnixMilli())
	if err != nil {
		if existing, loadErr := loadByWorkflow(ctx, tx, workflowJobID); loadErr == nil {
			if existing.ProjectID == projectID && existing.RepositoryID == repositoryID &&
				existing.BaseCommitSHA == baseCommitSHA && filepath.Clean(existing.Path) == workspacePath {
				return existing, SourceRepository{ID: repositoryID, LocalPath: localPath}, nil
			}
		}
		return Workspace{}, SourceRepository{}, fmt.Errorf("insert workspace: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, SourceRepository{}, fmt.Errorf("commit workspace ensure: %w", err)
	}
	return created, SourceRepository{ID: repositoryID, LocalPath: localPath}, nil
}

func (r *Repository) GetByWorkflow(ctx context.Context, workflowJobID string) (Workspace, error) {
	return loadByWorkflow(ctx, r.db, strings.TrimSpace(workflowJobID))
}

func (r *Repository) ListProject(ctx context.Context, projectID string) ([]Workspace, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+workspaceColumns+`
		FROM workspaces
		WHERE project_id = ?
		ORDER BY updated_at DESC, created_at DESC
	`, strings.TrimSpace(projectID))
	if err != nil {
		return nil, fmt.Errorf("list project workspaces: %w", err)
	}
	defer rows.Close()

	items := make([]Workspace, 0)
	for rows.Next() {
		item, err := scanWorkspace(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspaces: %w", err)
	}
	return items, nil
}

func (r *Repository) Acquire(ctx context.Context, workspaceID, owner string, ttl time.Duration) (Lease, error) {
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
			status = CASE WHEN status IN ('REQUESTED', 'FAILED') THEN 'PREPARING' ELSE status END,
			failure_message = CASE WHEN status = 'FAILED' THEN NULL ELSE failure_message END,
			updated_at = ?
		WHERE id = ?
			AND status IN ('REQUESTED', 'PREPARING', 'FAILED')
			AND (lease_token IS NULL OR lease_expires_at <= ? OR lease_owner = ?)
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
		return Lease{}, fmt.Errorf("acquire workspace lease: %w", err)
	}
	return Lease{Workspace: item, Token: token}, nil
}

func (r *Repository) Renew(ctx context.Context, workspaceID, token string, ttl time.Duration) (Lease, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(token) == "" || ttl <= 0 {
		return Lease{}, fmt.Errorf("%w: workspace ID, lease token, and positive TTL are required", ErrInvalidWorkspace)
	}
	now := r.now().UTC()
	expiresAt := now.Add(ttl)
	row := r.db.QueryRowContext(ctx, `
		UPDATE workspaces
		SET lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND lease_token = ? AND lease_expires_at > ?
		RETURNING `+workspaceColumns+`
	`, expiresAt.UnixMilli(), now.UnixMilli(), workspaceID, token, now.UnixMilli())
	item, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Lease{}, ErrLeaseLost
	}
	if err != nil {
		return Lease{}, fmt.Errorf("renew workspace lease: %w", err)
	}
	return Lease{Workspace: item, Token: token}, nil
}

func (r *Repository) Release(ctx context.Context, workspaceID, token string) error {
	now := r.now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE workspaces
		SET lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL, updated_at = ?
		WHERE id = ? AND lease_token = ?
	`, now.UnixMilli(), strings.TrimSpace(workspaceID), strings.TrimSpace(token))
	if err != nil {
		return fmt.Errorf("release workspace lease: %w", err)
	}
	return requireOneLease(result)
}

func (r *Repository) MarkReady(ctx context.Context, workspaceID, token, headSHA string, dirty bool) (Workspace, error) {
	headSHA = strings.TrimSpace(headSHA)
	if headSHA == "" {
		return Workspace{}, fmt.Errorf("%w: workspace HEAD is required", ErrInvalidWorkspace)
	}
	now := r.now().UTC()
	row := r.db.QueryRowContext(ctx, `
		UPDATE workspaces
		SET status = 'READY', head_sha = ?, dirty = ?, failure_message = NULL, updated_at = ?
		WHERE id = ? AND lease_token = ? AND lease_expires_at > ?
		RETURNING `+workspaceColumns+`
	`, headSHA, boolInteger(dirty), now.UnixMilli(), workspaceID, token, now.UnixMilli())
	item, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrLeaseLost
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("mark workspace ready: %w", err)
	}
	return item, nil
}

func (r *Repository) MarkFailed(ctx context.Context, workspaceID, token, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > 2048 {
		message = message[:2048]
	}
	now := r.now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE workspaces
		SET status = 'FAILED', failure_message = ?, updated_at = ?
		WHERE id = ? AND lease_token = ?
	`, nullableString(message), now.UnixMilli(), workspaceID, token)
	if err != nil {
		return fmt.Errorf("mark workspace failed: %w", err)
	}
	return requireOneLease(result)
}

func (r *Repository) getByID(ctx context.Context, workspaceID string) (Workspace, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = ?`, workspaceID)
	item, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("load workspace: %w", err)
	}
	return item, nil
}

const workspaceColumns = `
	id, workflow_job_id, project_id, repository_id, path,
	base_commit_sha, head_sha, status, dirty,
	lease_owner, lease_expires_at, failure_message,
	created_at, updated_at
`

func loadByWorkflow(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, workflowJobID string) (Workspace, error) {
	row := queryer.QueryRowContext(ctx, `
		SELECT `+workspaceColumns+`
		FROM workspaces WHERE workflow_job_id = ?
	`, workflowJobID)
	item, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("load workflow workspace: %w", err)
	}
	return item, nil
}

func scanWorkspace(row interface{ Scan(...any) error }) (Workspace, error) {
	var item Workspace
	var dirty int
	var leaseOwner, failureMessage sql.NullString
	var leaseExpiresAt sql.NullInt64
	var createdAt, updatedAt int64
	if err := row.Scan(
		&item.ID, &item.WorkflowJobID, &item.ProjectID, &item.RepositoryID, &item.Path,
		&item.BaseCommitSHA, &item.HeadSHA, &item.Status, &dirty,
		&leaseOwner, &leaseExpiresAt, &failureMessage,
		&createdAt, &updatedAt,
	); err != nil {
		return Workspace{}, err
	}
	item.Dirty = dirty == 1
	item.LeaseOwner = leaseOwner.String
	item.FailureMessage = failureMessage.String
	item.CreatedAt = time.UnixMilli(createdAt).UTC()
	item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	if leaseExpiresAt.Valid {
		value := time.UnixMilli(leaseExpiresAt.Int64).UTC()
		item.LeaseExpiresAt = &value
	}
	return item, nil
}

func validateIdentifier(value string) error {
	if value == "" || len(value) > 128 {
		return fmt.Errorf("%w: workflow job ID is invalid", ErrInvalidWorkspace)
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return fmt.Errorf("%w: workflow job ID contains unsafe characters", ErrInvalidWorkspace)
	}
	return nil
}

func requireOneLease(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read workspace lease rows: %w", err)
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate workspace ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
