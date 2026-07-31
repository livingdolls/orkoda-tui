package plans

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
	ErrNotFound        = errors.New("plan not found")
	ErrProjectNotFound = errors.New("project not found")
	ErrInvalidPlan     = errors.New("invalid plan")
)

type Status string

const (
	StatusDraft      Status = "DRAFT"
	StatusReady      Status = "READY"
	StatusPlanning   Status = "PLANNING"
	StatusNeedsInput Status = "NEEDS_INPUT"
	StatusApproved   Status = "APPROVED"
	StatusArchived   Status = "ARCHIVED"
)

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Version struct {
	ID                 string    `json:"id"`
	PlanID             string    `json:"plan_id"`
	Version            int       `json:"version"`
	Requirement        string    `json:"requirement"`
	AcceptanceCriteria []string  `json:"acceptance_criteria"`
	Constraints        []string  `json:"constraints"`
	CreatedAt          time.Time `json:"created_at"`
}

type Plan struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Title          string    `json:"title"`
	Status         Status    `json:"status"`
	CurrentVersion int       `json:"current_version"`
	Versions       []Version `json:"versions"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type VersionInput struct {
	Requirement        string
	AcceptanceCriteria []string
	Constraints        []string
}

type Repository struct {
	db       *sql.DB
	recorder EventRecorder
}

func NewRepository(db *sql.DB, recorder EventRecorder) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	return &Repository{db: db, recorder: recorder}, nil
}

func (r *Repository) Create(ctx context.Context, projectID, title string, input VersionInput) (Plan, error) {
	projectID = strings.TrimSpace(projectID)
	title = strings.TrimSpace(title)
	input = normalizeVersionInput(input)
	if projectID == "" || title == "" || input.Requirement == "" {
		return Plan{}, fmt.Errorf("%w: project, title, and requirement are required", ErrInvalidPlan)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, fmt.Errorf("begin plan creation: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, projectID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Plan{}, ErrProjectNotFound
		}
		return Plan{}, fmt.Errorf("check project: %w", err)
	}

	now := time.Now().UTC()
	plan := Plan{
		ID:             newID(),
		ProjectID:      projectID,
		Title:          title,
		Status:         StatusDraft,
		CurrentVersion: 1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	version := Version{
		ID:                 newID(),
		PlanID:             plan.ID,
		Version:            1,
		Requirement:        input.Requirement,
		AcceptanceCriteria: input.AcceptanceCriteria,
		Constraints:        input.Constraints,
		CreatedAt:          now,
	}

	criteriaJSON, constraintsJSON, err := marshalLists(version.AcceptanceCriteria, version.Constraints)
	if err != nil {
		return Plan{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO plans (id, project_id, title, status, current_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, ?)
	`, plan.ID, projectID, title, plan.Status, now.UnixMilli(), now.UnixMilli()); err != nil {
		return Plan{}, fmt.Errorf("insert plan: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO plan_versions (
			id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at
		) VALUES (?, ?, 1, ?, ?, ?, ?)
	`, version.ID, plan.ID, version.Requirement, criteriaJSON, constraintsJSON, now.UnixMilli()); err != nil {
		return Plan{}, fmt.Errorf("insert plan version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, fmt.Errorf("commit plan creation: %w", err)
	}

	plan.Versions = []Version{version}
	r.record(ctx, "plan.created", plan, now)
	r.record(ctx, "plan.version_created", map[string]any{
		"plan_id": plan.ID, "project_id": projectID, "version": 1,
	}, now)
	return plan, nil
}

func (r *Repository) AddVersion(ctx context.Context, planID string, input VersionInput) (Plan, error) {
	planID = strings.TrimSpace(planID)
	input = normalizeVersionInput(input)
	if planID == "" || input.Requirement == "" {
		return Plan{}, fmt.Errorf("%w: plan and requirement are required", ErrInvalidPlan)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, fmt.Errorf("begin plan version creation: %w", err)
	}
	defer tx.Rollback()

	var projectID string
	var currentVersion int
	if err := tx.QueryRowContext(ctx, `
		SELECT project_id, current_version FROM plans WHERE id = ?
	`, planID).Scan(&projectID, &currentVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Plan{}, ErrNotFound
		}
		return Plan{}, fmt.Errorf("read plan version: %w", err)
	}

	now := time.Now().UTC()
	nextVersion := currentVersion + 1
	criteriaJSON, constraintsJSON, err := marshalLists(input.AcceptanceCriteria, input.Constraints)
	if err != nil {
		return Plan{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO plan_versions (
			id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, newID(), planID, nextVersion, input.Requirement, criteriaJSON, constraintsJSON, now.UnixMilli()); err != nil {
		return Plan{}, fmt.Errorf("insert plan version: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE plans SET current_version = ?, updated_at = ?
		WHERE id = ? AND current_version = ?
	`, nextVersion, now.UnixMilli(), planID, currentVersion)
	if err != nil {
		return Plan{}, fmt.Errorf("advance plan version: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Plan{}, fmt.Errorf("read advanced plan count: %w", err)
	}
	if count != 1 {
		return Plan{}, fmt.Errorf("advance plan version: concurrent update detected")
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, fmt.Errorf("commit plan version: %w", err)
	}

	r.record(ctx, "plan.version_created", map[string]any{
		"plan_id": planID, "project_id": projectID, "version": nextVersion,
	}, now)
	return r.Get(ctx, planID)
}

func (r *Repository) ListProject(ctx context.Context, projectID string) ([]Plan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			p.id, p.project_id, p.title, p.status, p.current_version, p.created_at, p.updated_at,
			v.id, v.plan_id, v.version, v.requirement,
			v.acceptance_criteria_json, v.constraints_json, v.created_at
		FROM plans p
		JOIN plan_versions v ON v.plan_id = p.id AND v.version = p.current_version
		WHERE p.project_id = ?
		ORDER BY p.updated_at DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	result := make([]Plan, 0)
	for rows.Next() {
		plan, version, err := scanPlanWithVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		plan.Versions = []Version{version}
		result = append(result, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plans: %w", err)
	}
	return result, nil
}

func (r *Repository) Get(ctx context.Context, planID string) (Plan, error) {
	plan, err := scanPlan(r.db.QueryRowContext(ctx, `
		SELECT id, project_id, title, status, current_version, created_at, updated_at
		FROM plans WHERE id = ?
	`, planID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Plan{}, ErrNotFound
		}
		return Plan{}, fmt.Errorf("get plan: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at
		FROM plan_versions WHERE plan_id = ? ORDER BY version DESC
	`, planID)
	if err != nil {
		return Plan{}, fmt.Errorf("list plan versions: %w", err)
	}
	defer rows.Close()

	plan.Versions = make([]Version, 0, plan.CurrentVersion)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return Plan{}, fmt.Errorf("scan plan version: %w", err)
		}
		plan.Versions = append(plan.Versions, version)
	}
	if err := rows.Err(); err != nil {
		return Plan{}, fmt.Errorf("iterate plan versions: %w", err)
	}
	return plan, nil
}

func (r *Repository) Update(ctx context.Context, planID, title string, status Status) (Plan, error) {
	title = strings.TrimSpace(title)
	if title == "" || !validStatus(status) {
		return Plan{}, fmt.Errorf("%w: title and valid status are required", ErrInvalidPlan)
	}
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE plans SET title = ?, status = ?, updated_at = ? WHERE id = ?
	`, title, status, now.UnixMilli(), planID)
	if err != nil {
		return Plan{}, fmt.Errorf("update plan: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return Plan{}, err
	}

	eventType := "plan.updated"
	if status == StatusReady {
		eventType = "plan.ready"
	} else if status == StatusArchived {
		eventType = "plan.archived"
	}
	r.record(ctx, eventType, map[string]any{
		"plan_id": planID, "title": title, "status": status,
	}, now)
	return r.Get(ctx, planID)
}

func (r *Repository) Delete(ctx context.Context, planID string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, planID)
	if err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return err
	}
	r.record(ctx, "plan.archived", map[string]any{"plan_id": planID, "deleted": true}, time.Now().UTC())
	return nil
}

func (r *Repository) record(ctx context.Context, eventType string, payload any, createdAt time.Time) {
	if r.recorder == nil {
		return
	}
	if err := r.recorder.Record(ctx, "", eventType, payload, createdAt); err != nil {
		slog.Warn("record plan activity", "event_type", eventType, "error", err)
	}
}

func scanPlan(row interface{ Scan(...any) error }) (Plan, error) {
	var plan Plan
	var createdAt, updatedAt int64
	if err := row.Scan(
		&plan.ID, &plan.ProjectID, &plan.Title, &plan.Status,
		&plan.CurrentVersion, &createdAt, &updatedAt,
	); err != nil {
		return Plan{}, err
	}
	plan.CreatedAt = time.UnixMilli(createdAt).UTC()
	plan.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return plan, nil
}

func scanPlanWithVersion(row interface{ Scan(...any) error }) (Plan, Version, error) {
	var plan Plan
	var version Version
	var planCreatedAt, planUpdatedAt, versionCreatedAt int64
	var criteriaJSON, constraintsJSON string
	if err := row.Scan(
		&plan.ID, &plan.ProjectID, &plan.Title, &plan.Status,
		&plan.CurrentVersion, &planCreatedAt, &planUpdatedAt,
		&version.ID, &version.PlanID, &version.Version, &version.Requirement,
		&criteriaJSON, &constraintsJSON, &versionCreatedAt,
	); err != nil {
		return Plan{}, Version{}, err
	}
	if err := decodeLists(criteriaJSON, constraintsJSON, &version); err != nil {
		return Plan{}, Version{}, err
	}
	plan.CreatedAt = time.UnixMilli(planCreatedAt).UTC()
	plan.UpdatedAt = time.UnixMilli(planUpdatedAt).UTC()
	version.CreatedAt = time.UnixMilli(versionCreatedAt).UTC()
	return plan, version, nil
}

func scanVersion(row interface{ Scan(...any) error }) (Version, error) {
	var version Version
	var criteriaJSON, constraintsJSON string
	var createdAt int64
	if err := row.Scan(
		&version.ID, &version.PlanID, &version.Version, &version.Requirement,
		&criteriaJSON, &constraintsJSON, &createdAt,
	); err != nil {
		return Version{}, err
	}
	if err := decodeLists(criteriaJSON, constraintsJSON, &version); err != nil {
		return Version{}, err
	}
	version.CreatedAt = time.UnixMilli(createdAt).UTC()
	return version, nil
}

func decodeLists(criteriaJSON, constraintsJSON string, version *Version) error {
	if err := json.Unmarshal([]byte(criteriaJSON), &version.AcceptanceCriteria); err != nil {
		return fmt.Errorf("decode acceptance criteria: %w", err)
	}
	if err := json.Unmarshal([]byte(constraintsJSON), &version.Constraints); err != nil {
		return fmt.Errorf("decode constraints: %w", err)
	}
	return nil
}

func normalizeVersionInput(input VersionInput) VersionInput {
	input.Requirement = strings.TrimSpace(input.Requirement)
	input.AcceptanceCriteria = normalizeList(input.AcceptanceCriteria)
	input.Constraints = normalizeList(input.Constraints)
	return input
}

func normalizeList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func marshalLists(criteria, constraints []string) (string, string, error) {
	criteriaJSON, err := json.Marshal(criteria)
	if err != nil {
		return "", "", fmt.Errorf("marshal acceptance criteria: %w", err)
	}
	constraintsJSON, err := json.Marshal(constraints)
	if err != nil {
		return "", "", fmt.Errorf("marshal constraints: %w", err)
	}
	return string(criteriaJSON), string(constraintsJSON), nil
}

func requireAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected plans: %w", err)
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusDraft, StatusReady, StatusPlanning, StatusNeedsInput, StatusApproved, StatusArchived:
		return true
	default:
		return false
	}
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate plan ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
