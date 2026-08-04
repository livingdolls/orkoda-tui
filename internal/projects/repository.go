package projects

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/gitrepo"
)

var (
	ErrNotFound                    = errors.New("project not found")
	ErrInvalidProject              = errors.New("invalid project")
	ErrRepositoryAlreadyRegistered = errors.New("repository is already registered")
)

type Inspector interface {
	Inspect(context.Context, string) (gitrepo.Snapshot, error)
}

type BranchInspector interface {
	ListBranches(context.Context, string) ([]gitrepo.Branch, error)
}

type SubmoduleInspector interface {
	ListSubmodules(context.Context, string) ([]gitrepo.Submodule, error)
}

type RepositoryInfo struct {
	ID            string              `json:"id"`
	ProjectID     string              `json:"project_id"`
	LocalPath     string              `json:"local_path"`
	CurrentBranch string              `json:"current_branch"`
	HeadSHA       string              `json:"head_sha"`
	RemoteURL     string              `json:"remote_url,omitempty"`
	Dirty         bool                `json:"dirty"`
	TrustLevel    string              `json:"trust_level"`
	IgnorePolicy  json.RawMessage     `json:"ignore_policy"`
	Submodules    []gitrepo.Submodule `json:"submodules"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

type Project struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Repositories []RepositoryInfo `json:"repositories"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

type Repository struct {
	db        *sql.DB
	inspector Inspector
}

func NewRepository(db *sql.DB, inspector Inspector) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if inspector == nil {
		return nil, fmt.Errorf("Git inspector is required")
	}
	return &Repository{db: db, inspector: inspector}, nil
}

func (r *Repository) Create(ctx context.Context, name, repositoryPath string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, fmt.Errorf("%w: project name is required", ErrInvalidProject)
	}

	snapshot, err := r.inspector.Inspect(ctx, repositoryPath)
	if err != nil {
		return Project{}, fmt.Errorf("%w: inspect repository: %v", ErrInvalidProject, err)
	}
	if err := r.ensureRepositoryAvailable(ctx, snapshot.RootPath); err != nil {
		return Project{}, err
	}

	now := time.Now().UTC()
	project := Project{
		ID:        newID(),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	repository := RepositoryInfo{
		ID:            newID(),
		ProjectID:     project.ID,
		LocalPath:     snapshot.RootPath,
		CurrentBranch: snapshot.CurrentBranch,
		HeadSHA:       snapshot.HeadSHA,
		RemoteURL:     snapshot.RemoteURL,
		Dirty:         snapshot.Dirty,
		TrustLevel:    "UNTRUSTED",
		IgnorePolicy:  json.RawMessage(`{}`),
		Submodules:    []gitrepo.Submodule{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	submodulesJSON := `[]`
	if submoduleInspector, ok := r.inspector.(SubmoduleInspector); ok {
		submodules, submoduleErr := submoduleInspector.ListSubmodules(ctx, snapshot.RootPath)
		if submoduleErr != nil {
			return Project{}, fmt.Errorf("%w: inspect submodules: %v", ErrInvalidProject, submoduleErr)
		}
		repository.Submodules = submodules
		payload, marshalErr := json.Marshal(submodules)
		if marshalErr != nil {
			return Project{}, fmt.Errorf("marshal submodules: %w", marshalErr)
		}
		submodulesJSON = string(payload)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project creation: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO projects (id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`, project.ID, project.Name, now.UnixMilli(), now.UnixMilli()); err != nil {
		return Project{}, fmt.Errorf("insert project: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO repositories (
			id, project_id, local_path, current_branch, head_sha,
			remote_url, dirty, trust_level, ignore_policy_json, submodules_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, repository.ID, repository.ProjectID, repository.LocalPath,
		repository.CurrentBranch, repository.HeadSHA, repository.RemoteURL,
		boolToInteger(repository.Dirty), repository.TrustLevel, string(repository.IgnorePolicy), submodulesJSON, now.UnixMilli(), now.UnixMilli()); err != nil {
		return Project{}, fmt.Errorf("insert repository: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project creation: %w", err)
	}

	project.Repositories = []RepositoryInfo{repository}
	return project, nil
}

func (r *Repository) List(ctx context.Context) ([]Project, error) {
	return r.queryProjects(ctx, "", nil)
}

func (r *Repository) Get(ctx context.Context, projectID string) (Project, error) {
	projects, err := r.queryProjects(ctx, "WHERE p.id = ?", []any{projectID})
	if err != nil {
		return Project{}, err
	}
	if len(projects) == 0 {
		return Project{}, ErrNotFound
	}
	return projects[0], nil
}

func (r *Repository) Rename(ctx context.Context, projectID, name string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, fmt.Errorf("%w: project name is required", ErrInvalidProject)
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE projects SET name = ?, updated_at = ? WHERE id = ?
	`, name, time.Now().UTC().UnixMilli(), projectID)
	if err != nil {
		return Project{}, fmt.Errorf("rename project: %w", err)
	}
	if err := requireAffectedProject(result); err != nil {
		return Project{}, err
	}
	return r.Get(ctx, projectID)
}

func (r *Repository) Delete(ctx context.Context, projectID string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return requireAffectedProject(result)
}

func (r *Repository) Refresh(ctx context.Context, projectID string) (Project, error) {
	project, err := r.Get(ctx, projectID)
	if err != nil {
		return Project{}, err
	}

	type refreshedRepository struct {
		id       string
		snapshot gitrepo.Snapshot
	}
	refreshed := make([]refreshedRepository, 0, len(project.Repositories))
	for _, repository := range project.Repositories {
		snapshot, err := r.inspector.Inspect(ctx, repository.LocalPath)
		if err != nil {
			return Project{}, fmt.Errorf("%w: refresh repository %s: %v", ErrInvalidProject, repository.ID, err)
		}
		refreshed = append(refreshed, refreshedRepository{id: repository.ID, snapshot: snapshot})
	}

	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin repository refresh: %w", err)
	}
	defer tx.Rollback()

	for _, repository := range refreshed {
		submodulesJSON := `[]`
		if inspector, ok := r.inspector.(SubmoduleInspector); ok {
			submodules, submoduleErr := inspector.ListSubmodules(ctx, repository.snapshot.RootPath)
			if submoduleErr != nil {
				return Project{}, fmt.Errorf("%w: refresh submodules %s: %v", ErrInvalidProject, repository.id, submoduleErr)
			}
			payload, marshalErr := json.Marshal(submodules)
			if marshalErr != nil {
				return Project{}, fmt.Errorf("marshal submodules %s: %w", repository.id, marshalErr)
			}
			submodulesJSON = string(payload)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE repositories
			SET local_path = ?, current_branch = ?, head_sha = ?, remote_url = ?, dirty = ?, submodules_json = ?, updated_at = ?
			WHERE id = ? AND project_id = ?
		`, repository.snapshot.RootPath, repository.snapshot.CurrentBranch,
			repository.snapshot.HeadSHA, repository.snapshot.RemoteURL,
			boolToInteger(repository.snapshot.Dirty), submodulesJSON, now.UnixMilli(), repository.id, projectID); err != nil {
			return Project{}, fmt.Errorf("refresh repository %s: %w", repository.id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET updated_at = ? WHERE id = ?`, now.UnixMilli(), projectID); err != nil {
		return Project{}, fmt.Errorf("update project refresh time: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit repository refresh: %w", err)
	}
	return r.Get(ctx, projectID)
}

func (r *Repository) GetRepository(ctx context.Context, repositoryID string) (RepositoryInfo, error) {
	var item RepositoryInfo
	var dirty int
	var ignorePolicy, submodules string
	var createdAt, updatedAt int64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, project_id, local_path, current_branch, head_sha, remote_url,
			dirty, trust_level, ignore_policy_json, submodules_json, created_at, updated_at
		FROM repositories WHERE id = ?
	`, strings.TrimSpace(repositoryID)).Scan(
		&item.ID, &item.ProjectID, &item.LocalPath, &item.CurrentBranch, &item.HeadSHA,
		&item.RemoteURL, &dirty, &item.TrustLevel, &ignorePolicy, &submodules, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RepositoryInfo{}, ErrNotFound
	}
	if err != nil {
		return RepositoryInfo{}, fmt.Errorf("read repository: %w", err)
	}
	item.Dirty = dirty == 1
	item.IgnorePolicy = json.RawMessage(ignorePolicy)
	if err := json.Unmarshal([]byte(submodules), &item.Submodules); err != nil {
		return RepositoryInfo{}, fmt.Errorf("decode repository submodules: %w", err)
	}
	if item.Submodules == nil {
		item.Submodules = []gitrepo.Submodule{}
	}
	item.CreatedAt = time.UnixMilli(createdAt).UTC()
	item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return item, nil
}

func (r *Repository) ListBranches(ctx context.Context, repositoryID string) ([]gitrepo.Branch, error) {
	item, err := r.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	inspector, ok := r.inspector.(BranchInspector)
	if !ok {
		return nil, fmt.Errorf("Git branch inspection is unavailable")
	}
	return inspector.ListBranches(ctx, item.LocalPath)
}

func (r *Repository) Trust(ctx context.Context, repositoryID, level string, ignorePolicy map[string]any) (RepositoryInfo, error) {
	level = strings.ToUpper(strings.TrimSpace(level))
	if level != "UNTRUSTED" && level != "RESTRICTED" && level != "TRUSTED" {
		return RepositoryInfo{}, fmt.Errorf("%w: trust level must be UNTRUSTED, RESTRICTED, or TRUSTED", ErrInvalidProject)
	}
	if ignorePolicy == nil {
		ignorePolicy = map[string]any{}
	}
	policyJSON, err := json.Marshal(ignorePolicy)
	if err != nil {
		return RepositoryInfo{}, fmt.Errorf("marshal ignore policy: %w", err)
	}
	result, err := r.db.ExecContext(ctx, `UPDATE repositories SET trust_level = ?, ignore_policy_json = ?, updated_at = ? WHERE id = ?`, level, string(policyJSON), time.Now().UTC().UnixMilli(), strings.TrimSpace(repositoryID))
	if err != nil {
		return RepositoryInfo{}, fmt.Errorf("update repository trust: %w", err)
	}
	if err := requireAffectedProject(result); err != nil {
		return RepositoryInfo{}, err
	}
	return r.GetRepository(ctx, repositoryID)
}

func (r *Repository) ensureRepositoryAvailable(ctx context.Context, localPath string) error {
	var projectID string
	err := r.db.QueryRowContext(ctx, `SELECT project_id FROM repositories WHERE local_path = ?`, localPath).Scan(&projectID)
	if err == nil {
		return fmt.Errorf("%w: repository belongs to project %s", ErrRepositoryAlreadyRegistered, projectID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check repository registration: %w", err)
	}
	return nil
}

func (r *Repository) queryProjects(ctx context.Context, clause string, arguments []any) ([]Project, error) {
	query := `
		SELECT
			p.id, p.name, p.created_at, p.updated_at,
			r.id, r.project_id, r.local_path, r.current_branch, r.head_sha,
			r.remote_url, r.dirty, r.trust_level, r.ignore_policy_json, r.submodules_json, r.created_at, r.updated_at
		FROM projects p
		LEFT JOIN repositories r ON r.project_id = p.id
	` + clause + `
		ORDER BY p.created_at DESC, r.created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	projects := make([]Project, 0)
	indexes := make(map[string]int)
	for rows.Next() {
		var projectID, name string
		var projectCreatedAt, projectUpdatedAt int64
		var repositoryID, repositoryProjectID, localPath sql.NullString
		var currentBranch, headSHA, remoteURL, trustLevel, ignorePolicy, submodules sql.NullString
		var dirty, repositoryCreatedAt, repositoryUpdatedAt sql.NullInt64

		if err := rows.Scan(
			&projectID, &name, &projectCreatedAt, &projectUpdatedAt,
			&repositoryID, &repositoryProjectID, &localPath, &currentBranch, &headSHA,
			&remoteURL, &dirty, &trustLevel, &ignorePolicy, &submodules, &repositoryCreatedAt, &repositoryUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}

		index, exists := indexes[projectID]
		if !exists {
			index = len(projects)
			indexes[projectID] = index
			projects = append(projects, Project{
				ID:           projectID,
				Name:         name,
				Repositories: make([]RepositoryInfo, 0),
				CreatedAt:    time.UnixMilli(projectCreatedAt).UTC(),
				UpdatedAt:    time.UnixMilli(projectUpdatedAt).UTC(),
			})
		}
		if repositoryID.Valid {
			item := RepositoryInfo{
				ID:            repositoryID.String,
				ProjectID:     repositoryProjectID.String,
				LocalPath:     localPath.String,
				CurrentBranch: currentBranch.String,
				HeadSHA:       headSHA.String,
				RemoteURL:     remoteURL.String,
				Dirty:         dirty.Int64 == 1,
				TrustLevel:    trustLevel.String,
				IgnorePolicy:  json.RawMessage(ignorePolicy.String),
				CreatedAt:     time.UnixMilli(repositoryCreatedAt.Int64).UTC(),
				UpdatedAt:     time.UnixMilli(repositoryUpdatedAt.Int64).UTC(),
			}
			if item.TrustLevel == "" {
				item.TrustLevel = "UNTRUSTED"
			}
			if len(item.IgnorePolicy) == 0 {
				item.IgnorePolicy = json.RawMessage(`{}`)
			}
			if submodules.Valid && strings.TrimSpace(submodules.String) != "" {
				if err := json.Unmarshal([]byte(submodules.String), &item.Submodules); err != nil {
					return nil, fmt.Errorf("decode project submodules: %w", err)
				}
			}
			if item.Submodules == nil {
				item.Submodules = []gitrepo.Submodule{}
			}
			projects[index].Repositories = append(projects[index].Repositories, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return projects, nil
}

func requireAffectedProject(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected projects: %w", err)
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func boolToInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate project ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
