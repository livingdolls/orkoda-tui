package jobqueue

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestRecoverInterruptedRequeuesAllRunningJobsImmediately(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	queue := New(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	job, err := queue.Enqueue(ctx, "workflow.execute", `{}`, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.Claim(ctx, "local-daemon-old", now)
	if err != nil || claimed == nil {
		t.Fatalf("claim error=%v job=%#v", err, claimed)
	}
	recovered, err := queue.RecoverInterrupted(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0] != job.ID {
		t.Fatalf("recovered = %v", recovered)
	}
	next, err := queue.Claim(ctx, "local-daemon-new", now.Add(time.Second))
	if err != nil || next == nil || next.ID != job.ID || next.Attempts != 2 {
		t.Fatalf("reclaimed error=%v job=%#v", err, next)
	}
}
