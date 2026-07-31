package eventbus

import "testing"

func TestPublishAndUnsubscribe(t *testing.T) {
	bus := New()
	events, unsubscribe := bus.Subscribe(1)

	bus.Publish(Event{Sequence: 1, Type: "job.updated"})
	event := <-events
	if event.Sequence != 1 || event.Type != "job.updated" {
		t.Fatalf("event = %#v", event)
	}

	unsubscribe()
	if _, open := <-events; open {
		t.Fatal("subscription should be closed")
	}
}
