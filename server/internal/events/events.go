// Package events is the in-memory SSE pub/sub hub with replay.
package events

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Event is one server-sent event.
type Event struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Options configures a hub.
type Options struct {
	Buffer int
}

type subscription struct {
	ch       chan Event
	overflow bool
}

// Hub keeps a bounded history and fans events out to subscribers.
type Hub struct {
	mu   sync.Mutex
	ring []Event
	max  int
	next int
	subs map[chan Event]*subscription
	now  func() time.Time
}

// New returns a hub with the given history size (default 1000).
func New(opts Options) *Hub {
	if opts.Buffer <= 0 {
		opts.Buffer = 1000
	}
	return &Hub{
		ring: make([]Event, 0, opts.Buffer),
		max:  opts.Buffer,
		subs: make(map[chan Event]*subscription),
		now:  time.Now,
	}
}

// Publish records an event and delivers it to live subscribers.
func (h *Hub) Publish(kind string, data any) Event {
	payload, err := json.Marshal(data)
	if err != nil {
		payload = []byte("{}")
	}
	event := Event{ID: ulid.Make().String(), Type: kind, Data: payload}

	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.ring) < h.max {
		h.ring = append(h.ring, event)
	} else {
		h.ring[h.next] = event
		h.next = (h.next + 1) % h.max
	}
	for _, sub := range h.subs {
		if sub.overflow {
			// Once the client catches up, one resync replaces the lost events.
			select {
			case sub.ch <- Event{ID: event.ID, Type: "resync", Data: json.RawMessage(`{"reason":"slow_client"}`)}:
				sub.overflow = false
			default:
			}
			continue
		}
		select {
		case sub.ch <- event:
		default:
			sub.overflow = true
		}
	}
	return event
}

// Subscribe atomically registers a subscriber and returns the events after
// lastID. ok is false when lastID is unknown (the ring wrapped), in which case
// the caller must send a resync event.
func (h *Hub) Subscribe(lastID string) (replay []Event, ok bool, ch <-chan Event, cancel func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	replay, ok = h.replayLocked(lastID)
	sub := make(chan Event, 64)
	h.subs[sub] = &subscription{ch: sub}
	cancel = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, exists := h.subs[sub]; exists {
			delete(h.subs, sub)
			close(sub)
		}
	}
	return replay, ok, sub, cancel
}

func (h *Hub) replayLocked(lastID string) ([]Event, bool) {
	if lastID == "" {
		return nil, true
	}
	n := len(h.ring)
	start := 0
	if n == h.max {
		start = h.next
	}
	for k := 0; k < n; k++ {
		if h.ring[(start+k)%n].ID != lastID {
			continue
		}
		out := make([]Event, 0, n-k-1)
		for j := k + 1; j < n; j++ {
			out = append(out, h.ring[(start+j)%n])
		}
		return out, true
	}
	return nil, false
}

// Len reports how many events are retained.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.ring)
}
