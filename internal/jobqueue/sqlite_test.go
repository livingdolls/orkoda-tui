package jobqueue

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestQueueLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	queue := New(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	enqueued, err := queue.Enqueue(ctx, "execution.requested", `{"job_id":"job-1"}`, 2, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	claimed, err := queue.Claim(ctx, "local-daemon", now)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed == nil || claimed.ID != enqueued.ID || claimed.Attempts != 1 {
		t.Fatalf("claimed job = %#v", claimed)
	}

	status, err := queue.Fail(ctx, claimed.ID, "temporary error", now.Add(time.Second), now)
	if err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if status != "QUEUED" {
		t.Fatalf("status = %q", status)
	}

	claimed, err = queue.Claim(ctx, "local-daemon", now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("Claim() retry error = %v", err)
	}
	if claimed == nil || claimed.Attempts != 2 {
		t.Fatalf("retried job = %#v", claimed)
	}

	status, err = queue.Fail(ctx, claimed.ID, "permanent error", now, now)
	if err != nil {
		t.Fatalf("Fail() terminal error = %v", err)
	}
	if status != "DEAD" {
		t.Fatalf("terminal status = %q", status)
	}
}
