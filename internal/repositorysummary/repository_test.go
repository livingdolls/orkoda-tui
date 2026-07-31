package repositorysummary

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

type fakeScanner struct {
	calls int
}

func (f *fakeScanner) Scan(_ context.Context, rootPath, headSHA string) (Snapshot, error) {
	f.calls++
	return Snapshot{
		RootPath:        rootPath,
		HeadSHA:         headSHA,
		Languages:       []string{"Go"},
		Frameworks:      []string{"Gin"},
		PackageManagers: []string{"Go Modules"},
		Commands:        Commands{"test": {"go test ./..."}},
		ImportantFiles:  []string{"go.mod"},
		TopLevelEntries: []string{"cmd", "internal"},
		FileCount:       10,
	}, nil
}

func TestRepositoryGeneratesOncePerHeadAndPersistsHistory(t *testing.T) {
	ctx := context.Background()
	db := openSummaryTestDB(t)
	defer db.Close()
	seedRepository(t, db, "project-1", "repo-1", "head-1")

	scanner := &fakeScanner{}
	repository, err := NewRepository(db, scanner, nil)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}

	first, err := repository.Generate(ctx, "repo-1")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, err := repository.Generate(ctx, "repo-1")
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if first.ID != second.ID || scanner.calls != 1 {
		t.Fatalf("first=%#v second=%#v calls=%d", first, second, scanner.calls)
	}
	if first.ProjectID != "project-1" || first.Snapshot.FileCount != 10 {
		t.Fatalf("first = %#v", first)
	}

	if _, err := db.Exec(`UPDATE repositories SET head_sha = ?, dirty = 1 WHERE id = ?`, "head-2", "repo-1"); err != nil {
		t.Fatalf("update repository HEAD: %v", err)
	}
	if _, err := repository.Current(ctx, "repo-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Current() error = %v, want ErrNotFound", err)
	}
	third, err := repository.Generate(ctx, "repo-1")
	if err != nil {
		t.Fatalf("third Generate() error = %v", err)
	}
	if third.HeadSHA != "head-2" || !third.Dirty || scanner.calls != 2 {
		t.Fatalf("third=%#v calls=%d", third, scanner.calls)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repository_summaries WHERE repository_id = ?`, "repo-1").Scan(&count); err != nil {
		t.Fatalf("count summaries: %v", err)
	}
	if count != 2 {
		t.Fatalf("summary count = %d, want 2", count)
	}
}

func TestRepositorySummaryCascadesWithProject(t *testing.T) {
	ctx := context.Background()
	db := openSummaryTestDB(t)
	defer db.Close()
	seedRepository(t, db, "project-1", "repo-1", "head-1")

	repository, _ := NewRepository(db, &fakeScanner{}, nil)
	if _, err := repository.Generate(ctx, "repo-1"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, err := db.Exec(`DELETE FROM projects WHERE id = ?`, "project-1"); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repository_summaries`).Scan(&count); err != nil {
		t.Fatalf("count summaries: %v", err)
	}
	if count != 0 {
		t.Fatalf("summary count = %d, want 0", count)
	}
}

func openSummaryTestDB(t *testing.T) *sql.DB {
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

func seedRepository(t *testing.T, db *sql.DB, projectID, repositoryID, headSHA string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`, projectID, "Example", now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO repositories (
			id, project_id, local_path, current_branch, head_sha,
			remote_url, dirty, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, '', 0, ?, ?)
	`, repositoryID, projectID, "/tmp/example", "main", headSHA, now, now); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
}
