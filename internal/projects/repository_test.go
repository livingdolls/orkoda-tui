package projects

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/gitrepo"
)

type fakeInspector struct {
	snapshots  map[string]gitrepo.Snapshot
	branches   map[string][]gitrepo.Branch
	submodules map[string][]gitrepo.Submodule
	err        error
}

func (f *fakeInspector) ListBranches(_ context.Context, path string) ([]gitrepo.Branch, error) {
	return append([]gitrepo.Branch(nil), f.branches[path]...), nil
}

func (f *fakeInspector) ListSubmodules(_ context.Context, path string) ([]gitrepo.Submodule, error) {
	return append([]gitrepo.Submodule(nil), f.submodules[path]...), nil
}

func (f *fakeInspector) Inspect(_ context.Context, path string) (gitrepo.Snapshot, error) {
	if f.err != nil {
		return gitrepo.Snapshot{}, f.err
	}
	if snapshot, ok := f.snapshots[path]; ok {
		return snapshot, nil
	}
	return gitrepo.Snapshot{}, errors.New("repository not configured")
}

func openTestRepository(t *testing.T, inspector Inspector) (*Repository, *sql.DB) {
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
	repository, err := NewRepository(db, inspector)
	if err != nil {
		db.Close()
		t.Fatalf("NewRepository() error = %v", err)
	}
	return repository, db
}

func TestCreateAndListProject(t *testing.T) {
	path := "/tmp/example"
	inspector := &fakeInspector{snapshots: map[string]gitrepo.Snapshot{
		path: {
			RootPath:      path,
			CurrentBranch: "main",
			HeadSHA:       "abc123",
			RemoteURL:     "git@example.com:repo.git",
			Dirty:         true,
		},
	}}
	repository, db := openTestRepository(t, inspector)
	defer db.Close()

	created, err := repository.Create(context.Background(), " Example ", path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name != "Example" || len(created.Repositories) != 1 {
		t.Fatalf("created = %#v", created)
	}
	if !created.Repositories[0].Dirty || created.Repositories[0].HeadSHA != "abc123" {
		t.Fatalf("repository = %#v", created.Repositories[0])
	}

	projects, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(projects) != 1 || projects[0].ID != created.ID {
		t.Fatalf("projects = %#v", projects)
	}
}

func TestRepositoryMetadataBranchesTrustAndSubmodules(t *testing.T) {
	path := "/tmp/metadata"
	branches := []gitrepo.Branch{{Name: "main", HeadSHA: "abc123", Current: true}, {Name: "feature/x", HeadSHA: "def456"}}
	submodules := []gitrepo.Submodule{{Path: "vendor/library", Commit: "0123456789abcdef", URL: "https://example.invalid/library.git"}}
	inspector := &fakeInspector{
		snapshots:  map[string]gitrepo.Snapshot{path: {RootPath: path, CurrentBranch: "main", HeadSHA: "abc123"}},
		branches:   map[string][]gitrepo.Branch{path: branches},
		submodules: map[string][]gitrepo.Submodule{path: submodules},
	}
	repository, db := openTestRepository(t, inspector)
	defer db.Close()
	created, err := repository.Create(context.Background(), "Metadata", path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.Repositories[0].Submodules, submodules) {
		t.Fatalf("created submodules = %#v", created.Repositories[0].Submodules)
	}
	listed, err := repository.ListBranches(context.Background(), created.Repositories[0].ID)
	if err != nil || !reflect.DeepEqual(listed, branches) {
		t.Fatalf("ListBranches() = %#v, %v", listed, err)
	}
	trusted, err := repository.Trust(context.Background(), created.Repositories[0].ID, "trusted", map[string]any{"paths": []any{"vendor"}})
	if err != nil {
		t.Fatal(err)
	}
	if trusted.TrustLevel != "TRUSTED" || string(trusted.IgnorePolicy) != `{"paths":["vendor"]}` {
		t.Fatalf("trusted repository = %#v", trusted)
	}
}

func TestCreateRejectsDuplicateRepository(t *testing.T) {
	path := "/tmp/example"
	inspector := &fakeInspector{snapshots: map[string]gitrepo.Snapshot{
		path: {RootPath: path, HeadSHA: "abc123"},
	}}
	repository, db := openTestRepository(t, inspector)
	defer db.Close()

	if _, err := repository.Create(context.Background(), "First", path); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if _, err := repository.Create(context.Background(), "Second", path); !errors.Is(err, ErrRepositoryAlreadyRegistered) {
		t.Fatalf("second Create() error = %v", err)
	}
}

func TestRenameRefreshAndDeleteProject(t *testing.T) {
	path := "/tmp/example"
	inspector := &fakeInspector{snapshots: map[string]gitrepo.Snapshot{
		path: {RootPath: path, CurrentBranch: "main", HeadSHA: "abc123"},
	}}
	repository, db := openTestRepository(t, inspector)
	defer db.Close()

	created, err := repository.Create(context.Background(), "Example", path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	renamed, err := repository.Rename(context.Background(), created.ID, "Renamed")
	if err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if renamed.Name != "Renamed" {
		t.Fatalf("renamed.Name = %q", renamed.Name)
	}

	inspector.snapshots[path] = gitrepo.Snapshot{
		RootPath:      path,
		CurrentBranch: "feature/test",
		HeadSHA:       "def456",
		Dirty:         true,
	}
	refreshed, err := repository.Refresh(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.Repositories[0].HeadSHA != "def456" || !refreshed.Repositories[0].Dirty {
		t.Fatalf("refreshed = %#v", refreshed)
	}

	if err := repository.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repository.Get(context.Background(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE project_id = ?`, created.ID).Scan(&count); err != nil {
		t.Fatalf("count repositories: %v", err)
	}
	if count != 0 {
		t.Fatalf("repository count = %d", count)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	repository, db := openTestRepository(t, &fakeInspector{err: errors.New("not git")})
	defer db.Close()

	if _, err := repository.Create(context.Background(), "", "/tmp/example"); !errors.Is(err, ErrInvalidProject) {
		t.Fatalf("empty name error = %v", err)
	}
	if _, err := repository.Create(context.Background(), "Example", "/tmp/example"); !errors.Is(err, ErrInvalidProject) {
		t.Fatalf("invalid repository error = %v", err)
	}
}
