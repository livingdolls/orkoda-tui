package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestWriteLeaseAcquireRenewReleaseAndTakeover(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedWorkspaceWorkflow(t, db)

	repository, err := NewRepository(db, filepath.Join(t.TempDir(), "workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	current := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	item, _, err := repository.EnsureForWorkflow(ctx, "workflow-1")
	if err != nil {
		t.Fatal(err)
	}
	prepare, err := repository.Acquire(ctx, item.ID, "prepare-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MarkReady(ctx, item.ID, prepare.Token, "abc123", false); err != nil {
		t.Fatal(err)
	}
	if err := repository.Release(ctx, item.ID, prepare.Token); err != nil {
		t.Fatal(err)
	}

	write, err := repository.AcquireWrite(ctx, item.ID, "executor-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if write.Workspace.Status != StatusWriteLocked || write.Token == "" {
		t.Fatalf("write lease = %#v", write)
	}
	if _, err := repository.AcquireWrite(ctx, item.ID, "executor-b", time.Minute); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("competing write lease error = %v", err)
	}
	current = current.Add(20 * time.Second)
	renewed, err := repository.Renew(ctx, item.ID, write.Token, time.Minute)
	if err != nil || renewed.Workspace.LeaseExpiresAt == nil {
		t.Fatalf("Renew() = %#v, %v", renewed, err)
	}
	if _, err := repository.ReleaseWrite(ctx, item.ID, "wrong-token", "abc123", true); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale ReleaseWrite() error = %v", err)
	}
	released, err := repository.ReleaseWrite(ctx, item.ID, write.Token, "def456", true)
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != StatusReady || released.HeadSHA != "def456" || !released.Dirty || released.LeaseOwner != "" {
		t.Fatalf("released workspace = %#v", released)
	}

	second, err := repository.AcquireWrite(ctx, item.ID, "executor-b", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(2 * time.Second)
	takeover, err := repository.AcquireWrite(ctx, item.ID, "executor-c", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if takeover.Token == second.Token || takeover.Workspace.LeaseOwner != "executor-c" {
		t.Fatalf("takeover = %#v", takeover)
	}
	if _, err := repository.Renew(ctx, item.ID, second.Token, time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale Renew() error = %v", err)
	}
}
