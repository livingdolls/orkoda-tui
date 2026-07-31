package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/activity"
)

type fakeEventReader struct {
	events        []activity.Event
	lastJobID     string
	lastAfter     int64
	lastLimit     int
	jobFilterUsed bool
}

func (f *fakeEventReader) ListAfter(_ context.Context, after int64, limit int) ([]activity.Event, error) {
	f.lastAfter = after
	f.lastLimit = limit
	return append([]activity.Event(nil), f.events...), nil
}

func (f *fakeEventReader) ListJobAfter(_ context.Context, jobID string, after int64, limit int) ([]activity.Event, error) {
	f.lastJobID = jobID
	f.lastAfter = after
	f.lastLimit = limit
	f.jobFilterUsed = true
	return append([]activity.Event(nil), f.events...), nil
}

func TestReplayEventsReturnsPaginationMetadata(t *testing.T) {
	reader := &fakeEventReader{events: []activity.Event{
		{Sequence: 11, JobID: "job-a", Type: "job.started", PayloadJSON: json.RawMessage(`{"attempt":1}`), CreatedAt: time.Unix(1, 0).UTC()},
		{Sequence: 12, JobID: "job-a", Type: "job.completed", PayloadJSON: json.RawMessage(`{"attempt":1}`), CreatedAt: time.Unix(2, 0).UTC()},
		{Sequence: 13, JobID: "job-b", Type: "job.started", PayloadJSON: json.RawMessage(`{}`), CreatedAt: time.Unix(3, 0).UTC()},
	}}
	router := NewRouter("development", reader, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?after_sequence=10&limit=2", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if reader.lastAfter != 10 || reader.lastLimit != 3 {
		t.Fatalf("reader args after=%d limit=%d", reader.lastAfter, reader.lastLimit)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"last_sequence":12`) || !strings.Contains(body, `"has_more":true`) {
		t.Fatalf("pagination metadata missing: %s", body)
	}
	if !strings.Contains(body, `"payload":{"attempt":1}`) {
		t.Fatalf("payload was encoded as a string: %s", body)
	}
}

func TestReplayJobEventsUsesJobFilter(t *testing.T) {
	reader := &fakeEventReader{}
	router := NewRouter("development", reader, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/job-42/events", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !reader.jobFilterUsed || reader.lastJobID != "job-42" {
		t.Fatalf("job filter used=%v jobID=%q", reader.jobFilterUsed, reader.lastJobID)
	}
}

func TestReplayEventsRejectsInvalidQuery(t *testing.T) {
	router := NewRouter("development", &fakeEventReader{}, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?after_sequence=-1&limit=0", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
