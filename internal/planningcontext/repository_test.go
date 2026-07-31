package planningcontext

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/repositorysummary"
)

type fakeSummaryReader struct {
	summary repositorysummary.Summary
	err     error
}

func (f fakeSummaryReader) Current(context.Context, string) (repositorysummary.Summary, error) {
	return f.summary, f.err
}

func TestNormalizeBindsContextToPlanVersionAndRepositoryHead(t *testing.T) {
	ctx := context.Background()
	db := openPlanningTestDB(t)
	defer db.Close()
	seedPlanningState(t, db)

	summary := repositorysummary.Summary{
		ID:           "summary-1",
		RepositoryID: "repo-1",
		ProjectID:    "project-1",
		HeadSHA:      "head-1",
		Dirty:        true,
		Snapshot: repositorysummary.Snapshot{
			HeadSHA:         "head-1",
			Languages:       []string{"Go", "TypeScript"},
			Frameworks:      []string{"Gin", "OpenTUI"},
			PackageManagers: []string{"Bun", "Go Modules"},
			Commands:        repositorysummary.Commands{"test": {"go test ./..."}},
			ImportantFiles:  []string{"go.mod", "package.json"},
		},
	}
	repository, err := NewRepository(db, fakeSummaryReader{summary: summary}, nil)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}

	first, err := repository.Normalize(ctx, "plan-1")
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if first.PlanVersion != 1 || first.RepositorySummaryID != "summary-1" {
		t.Fatalf("first = %#v", first)
	}
	if !slices.Contains(first.NormalizedPlan.AffectedAreas, "backend") || !slices.Contains(first.NormalizedPlan.AffectedAreas, "frontend") {
		t.Fatalf("affected areas = %#v", first.NormalizedPlan.AffectedAreas)
	}
	if len(first.NormalizedPlan.Risks) == 0 || len(first.NormalizedPlan.OpenQuestions) == 0 {
		t.Fatalf("normalized plan = %#v", first.NormalizedPlan)
	}

	second, err := repository.Normalize(ctx, "plan-1")
	if err != nil {
		t.Fatalf("second Normalize() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second ID = %q, want %q", second.ID, first.ID)
	}

	now := time.Now().UTC().UnixMilli()
	if _, err := db.Exec(`
		INSERT INTO plan_versions (
			id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at
		) VALUES ('version-2', 'plan-1', 2, 'Add search', '["Search works"]', '["Use existing stack"]', ?)
	`, now); err != nil {
		t.Fatalf("insert version 2: %v", err)
	}
	if _, err := db.Exec(`UPDATE plans SET current_version = 2, updated_at = ? WHERE id = 'plan-1'`, now); err != nil {
		t.Fatalf("advance plan: %v", err)
	}

	third, err := repository.Normalize(ctx, "plan-1")
	if err != nil {
		t.Fatalf("third Normalize() error = %v", err)
	}
	if third.PlanVersion != 2 || third.PlanVersionID != "version-2" || third.ID == first.ID {
		t.Fatalf("third = %#v", third)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM planning_contexts WHERE plan_id = 'plan-1'`).Scan(&count); err != nil {
		t.Fatalf("count contexts: %v", err)
	}
	if count != 2 {
		t.Fatalf("context count = %d, want 2", count)
	}
}

func TestNormalizeRequiresCurrentRepositorySummary(t *testing.T) {
	db := openPlanningTestDB(t)
	defer db.Close()
	seedPlanningState(t, db)

	repository, _ := NewRepository(db, fakeSummaryReader{err: repositorysummary.ErrNotFound}, nil)
	if _, err := repository.Normalize(context.Background(), "plan-1"); !errors.Is(err, ErrSummaryMissing) {
		t.Fatalf("Normalize() error = %v, want ErrSummaryMissing", err)
	}
}

func openPlanningTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatalf("database.Migrate() error = %v", err)
	}
	return db
}

func seedPlanningState(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, created_at, updated_at) VALUES ('project-1', 'Example', ?, ?)`, []any{now, now}},
		{`INSERT INTO repositories (
			id, project_id, local_path, current_branch, head_sha, remote_url, dirty, created_at, updated_at
		) VALUES ('repo-1', 'project-1', '/tmp/example', 'main', 'head-1', '', 1, ?, ?)`, []any{now, now}},
		{`INSERT INTO plans (
			id, project_id, title, status, current_version, created_at, updated_at
		) VALUES ('plan-1', 'project-1', 'Add blog', 'DRAFT', 1, ?, ?)`, []any{now, now}},
		{`INSERT INTO plan_versions (
			id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at
		) VALUES ('version-1', 'plan-1', 1, 'Add a Markdown blog', '[]', '[]', ?)`, []any{now}},
		{`INSERT INTO repository_summaries (
			id, repository_id, head_sha, summary_json, created_at
		) VALUES ('summary-1', 'repo-1', 'head-1', '{}', ?)`, []any{now}},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed planning state: %v", err)
		}
	}
}
