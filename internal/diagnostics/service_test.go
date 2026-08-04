package diagnostics

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/artifact"
	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestServiceReadsHealthAndWritesBundle(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store, err := artifact.NewLocalStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db, store)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "ready" || snapshot.Database.Integrity != "ok" || snapshot.Database.Schema == 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	key, err := service.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || len(content) == 0 {
		t.Fatalf("bundle content length = %d, err = %v", len(content), err)
	}
}
