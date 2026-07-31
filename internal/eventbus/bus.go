package eventbus

import "sync"

type Event struct {
	Sequence int64
	Type     string
	Payload  any
}

type Bus struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]chan Event
	closed      bool
}

func New() *Bus {
	return &Bus{subscribers: make(map[uint64]chan Event)}
}

func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	channel := make(chan Event, buffer)
	if b.closed {
		close(channel)
		return channel, func() {}
	}

	id := b.nextID
	b.nextID++
	b.subscribers[id] = channel

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if current, ok := b.subscribers[id]; ok {
				delete(b.subscribers, id)
				close(current)
			}
		})
	}
	return channel, unsubscribe
}

// Publish is deliberately non-blocking. Durable events must be written to
// SQLite before publishing; slow TUI subscribers can replay missed events.
func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}
	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	for id, subscriber := range b.subscribers {
		close(subscriber)
		delete(b.subscribers, id)
	}
}
