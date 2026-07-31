package activity

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/eventbus"
)

func TestRepositoryReplaySequenceFilterAndPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orkoda.db")
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := NewRepository(db)
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	first, err := repository.Append(ctx, "job-a", "job.started", json.RawMessage(`{"attempt":1}`), now)
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}
	second, err := repository.Append(ctx, "job-b", "job.started", json.RawMessage(`{"attempt":1}`), now.Add(time.Second))
	if err != nil {
		t.Fatalf("append second event: %v", err)
	}
	third, err := repository.Append(ctx, "job-a", "job.completed", json.RawMessage(`{"attempt":1}`), now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("append third event: %v", err)
	}
	if !(first.Sequence < second.Sequence && second.Sequence < third.Sequence) {
		t.Fatalf("sequences are not increasing: %d, %d, %d", first.Sequence, second.Sequence, third.Sequence)
	}

	events, err := repository.ListAfter(ctx, first.Sequence, 10)
	if err != nil {
		t.Fatalf("list after sequence: %v", err)
	}
	if len(events) != 2 || events[0].Sequence != second.Sequence || events[1].Sequence != third.Sequence {
		t.Fatalf("replayed events = %#v", events)
	}

	jobEvents, err := repository.ListJobAfter(ctx, "job-a", 0, 10)
	if err != nil {
		t.Fatalf("list job events: %v", err)
	}
	if len(jobEvents) != 2 || jobEvents[0].JobID != "job-a" || jobEvents[1].Type != "job.completed" {
		t.Fatalf("job events = %#v", jobEvents)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	db, err = database.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()

	repository = NewRepository(db)
	persisted, err := repository.ListAfter(ctx, 0, 10)
	if err != nil {
		t.Fatalf("list persisted events: %v", err)
	}
	if len(persisted) != 3 {
		t.Fatalf("persisted event count = %d", len(persisted))
	}
}

func TestRepositoryPaginationDoesNotDuplicateEvents(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := NewRepository(db)
	for index := 0; index < 5; index++ {
		if _, err := repository.Append(ctx, "job-a", "job.progress", json.RawMessage(`{}`), time.Now()); err != nil {
			t.Fatalf("append event %d: %v", index, err)
		}
	}

	firstPage, err := repository.ListAfter(ctx, 0, 2)
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	secondPage, err := repository.ListAfter(ctx, firstPage[len(firstPage)-1].Sequence, 2)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if firstPage[1].Sequence >= secondPage[0].Sequence {
		t.Fatalf("pages overlap: first=%d second=%d", firstPage[1].Sequence, secondPage[0].Sequence)
	}
}

type failingAppender struct{}

func (failingAppender) Append(context.Context, string, string, json.RawMessage, time.Time) (Event, error) {
	return Event{}, errors.New("insert failed")
}

type recordingPublisher struct {
	events []eventbus.Event
}

func (p *recordingPublisher) Publish(event eventbus.Event) {
	p.events = append(p.events, event)
}

func TestRecorderDoesNotPublishWhenAppendFails(t *testing.T) {
	publisher := &recordingPublisher{}
	recorder, err := NewRecorder(failingAppender{}, publisher)
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	if err := recorder.Record(context.Background(), "job-a", "job.started", map[string]int{"attempt": 1}, time.Now()); err == nil {
		t.Fatal("Record() expected an error")
	}
	if len(publisher.events) != 0 {
		t.Fatalf("published events = %d", len(publisher.events))
	}
}
