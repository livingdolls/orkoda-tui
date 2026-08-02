package jobqueue

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestEnqueueTxCommitsAndRollsBackWithCaller(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("database.Migrate() error = %v", err)
	}
	queue := New(db)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	rolledBack, err := queue.EnqueueTx(ctx, tx, "workflow.execute", `{}`, 3, time.Now())
	if err != nil {
		t.Fatalf("EnqueueTx() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE id = ?`, rolledBack.ID).Scan(&count); err != nil {
		t.Fatalf("count rolled back job: %v", err)
	}
	if count != 0 {
		t.Fatalf("rolled back job count = %d", count)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	committed, err := queue.EnqueueTx(ctx, tx, "workflow.execute", `{}`, 3, time.Now())
	if err != nil {
		t.Fatalf("EnqueueTx() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE id = ?`, committed.ID).Scan(&count); err != nil {
		t.Fatalf("count committed job: %v", err)
	}
	if count != 1 {
		t.Fatalf("committed job count = %d", count)
	}
}

func TestClaimTypesLeavesUnsupportedJobsQueued(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("database.Migrate() error = %v", err)
	}
	queue := New(db)
	now := time.Now().UTC()
	workflow, err := queue.Enqueue(ctx, "workflow.execute", `{}`, 3, now)
	if err != nil {
		t.Fatalf("enqueue workflow job: %v", err)
	}
	noop, err := queue.Enqueue(ctx, "system.noop", `{}`, 3, now.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("enqueue noop job: %v", err)
	}

	claimed, err := queue.ClaimTypes(ctx, "worker", now.Add(time.Second), []string{"system.noop", "system.noop", ""})
	if err != nil {
		t.Fatalf("ClaimTypes() error = %v", err)
	}
	if claimed == nil || claimed.ID != noop.ID {
		t.Fatalf("claimed = %#v", claimed)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, workflow.ID).Scan(&status); err != nil {
		t.Fatalf("read unsupported status: %v", err)
	}
	if status != "QUEUED" {
		t.Fatalf("unsupported status = %q", status)
	}

	none, err := queue.ClaimTypes(ctx, "worker", now.Add(time.Second), nil)
	if err != nil {
		t.Fatalf("ClaimTypes(empty) error = %v", err)
	}
	if none != nil {
		t.Fatalf("empty claim = %#v", none)
	}
}
