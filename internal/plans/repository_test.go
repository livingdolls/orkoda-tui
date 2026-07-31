package plans

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

type recordedEvent struct {
	eventType string
	payload   any
}

type fakeRecorder struct {
	events []recordedEvent
}

func (f *fakeRecorder) Record(_ context.Context, _ string, eventType string, payload any, _ time.Time) error {
	f.events = append(f.events, recordedEvent{eventType: eventType, payload: payload})
	return nil
}

func openPlanRepository(t *testing.T) (*Repository, *sql.DB, *fakeRecorder, string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatalf("database.Migrate() error = %v", err)
	}

	projectID := "project-1"
	now := time.Now().UTC().UnixMilli()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)
	`, projectID, "Example", now, now); err != nil {
		db.Close()
		t.Fatalf("insert project: %v", err)
	}

	recorder := &fakeRecorder{}
	repository, err := NewRepository(db, recorder)
	if err != nil {
		db.Close()
		t.Fatalf("NewRepository() error = %v", err)
	}
	return repository, db, recorder, projectID
}

func TestCreatePlanAndAddImmutableVersion(t *testing.T) {
	repository, db, recorder, projectID := openPlanRepository(t)
	defer db.Close()
	ctx := context.Background()

	created, err := repository.Create(ctx, projectID, " Blog feature ", VersionInput{
		Requirement:        " Build a Markdown blog. ",
		AcceptanceCriteria: []string{"Lists articles", "", " Opens detail "},
		Constraints:        []string{"Use existing stack"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Title != "Blog feature" || created.Status != StatusDraft || created.CurrentVersion != 1 {
		t.Fatalf("created = %#v", created)
	}
	if got := created.Versions[0].AcceptanceCriteria; len(got) != 2 || got[1] != "Opens detail" {
		t.Fatalf("acceptance criteria = %#v", got)
	}

	updated, err := repository.AddVersion(ctx, created.ID, VersionInput{
		Requirement:        "Build a Markdown blog with search.",
		AcceptanceCriteria: []string{"Lists articles", "Searches articles"},
		Constraints:        []string{"Use existing stack"},
	})
	if err != nil {
		t.Fatalf("AddVersion() error = %v", err)
	}
	if updated.CurrentVersion != 2 || len(updated.Versions) != 2 {
		t.Fatalf("updated = %#v", updated)
	}
	if updated.Versions[0].Version != 2 || updated.Versions[1].Requirement != "Build a Markdown blog." {
		t.Fatalf("versions = %#v", updated.Versions)
	}
	if len(recorder.events) != 3 {
		t.Fatalf("recorded events = %#v", recorder.events)
	}
	if recorder.events[0].eventType != "plan.created" || recorder.events[2].eventType != "plan.version_created" {
		t.Fatalf("recorded events = %#v", recorder.events)
	}
}

func TestListUpdateAndDeletePlan(t *testing.T) {
	repository, db, recorder, projectID := openPlanRepository(t)
	defer db.Close()
	ctx := context.Background()

	created, err := repository.Create(ctx, projectID, "Feature", VersionInput{Requirement: "Build it"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	listed, err := repository.ListProject(ctx, projectID)
	if err != nil {
		t.Fatalf("ListProject() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Versions[0].Requirement != "Build it" {
		t.Fatalf("listed = %#v", listed)
	}

	ready, err := repository.Update(ctx, created.ID, "Feature ready", StatusReady)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if ready.Title != "Feature ready" || ready.Status != StatusReady {
		t.Fatalf("ready = %#v", ready)
	}
	if recorder.events[len(recorder.events)-1].eventType != "plan.ready" {
		t.Fatalf("events = %#v", recorder.events)
	}

	if err := repository.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repository.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestProjectDeleteCascadesPlanVersions(t *testing.T) {
	repository, db, _, projectID := openPlanRepository(t)
	defer db.Close()
	ctx := context.Background()

	created, err := repository.Create(ctx, projectID, "Feature", VersionInput{Requirement: "Build it"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := repository.AddVersion(ctx, created.ID, VersionInput{Requirement: "Build it better"}); err != nil {
		t.Fatalf("AddVersion() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	var plansCount, versionsCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plans WHERE project_id = ?`, projectID).Scan(&plansCount); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_versions WHERE plan_id = ?`, created.ID).Scan(&versionsCount); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if plansCount != 0 || versionsCount != 0 {
		t.Fatalf("plans=%d versions=%d", plansCount, versionsCount)
	}
}

func TestPlanValidationAndMissingRecords(t *testing.T) {
	repository, db, _, projectID := openPlanRepository(t)
	defer db.Close()
	ctx := context.Background()

	if _, err := repository.Create(ctx, projectID, "", VersionInput{Requirement: "Build"}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("empty title error = %v", err)
	}
	if _, err := repository.Create(ctx, projectID, "Feature", VersionInput{}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("empty requirement error = %v", err)
	}
	if _, err := repository.Create(ctx, "missing", "Feature", VersionInput{Requirement: "Build"}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project error = %v", err)
	}
	if _, err := repository.AddVersion(ctx, "missing", VersionInput{Requirement: "Build"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing plan error = %v", err)
	}
}
