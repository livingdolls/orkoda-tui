package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/eventbus"
)

type Appender interface {
	Append(context.Context, string, string, json.RawMessage, time.Time) (Event, error)
}

type Publisher interface {
	Publish(eventbus.Event)
}

type Recorder struct {
	appender  Appender
	publisher Publisher
}

func NewRecorder(appender Appender, publisher Publisher) (*Recorder, error) {
	if appender == nil {
		return nil, fmt.Errorf("activity appender is required")
	}
	return &Recorder{appender: appender, publisher: publisher}, nil
}

func (r *Recorder) Record(ctx context.Context, jobID, eventType string, payload any, createdAt time.Time) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal activity payload: %w", err)
	}

	event, err := r.appender.Append(ctx, jobID, eventType, payloadJSON, createdAt)
	if err != nil {
		return err
	}

	if r.publisher != nil {
		r.publisher.Publish(eventbus.Event{
			Sequence: event.Sequence,
			JobID:    event.JobID,
			Type:     event.Type,
			Payload:  append(json.RawMessage(nil), event.PayloadJSON...),
		})
	}
	return nil
}
