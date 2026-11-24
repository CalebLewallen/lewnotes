package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/events"
)

const sseHeartbeat = 20 * time.Second

func handleEvents(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.Events == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "events_unavailable", "The event stream is not available.")
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeProblem(w, r, http.StatusInternalServerError, "streaming_unavailable", "Streaming is not supported.")
			return
		}

		lastID := r.Header.Get("Last-Event-ID")
		if lastID == "" {
			lastID = r.URL.Query().Get("last_event_id")
		}
		replay, known, ch, cancel := opts.Events.Subscribe(lastID)
		defer cancel()

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-store")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		if !known {
			writeSSE(w, events.Event{Type: "resync", Data: json.RawMessage(`{"reason":"unknown_last_event_id"}`)})
		}
		for _, event := range replay {
			writeSSE(w, event)
		}
		_, _ = fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()

		ticker := time.NewTicker(sseHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				writeSSE(w, event)
				flusher.Flush()
			case <-ticker.C:
				_, _ = fmt.Fprint(w, ": heartbeat\n\n")
				flusher.Flush()
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, event events.Event) {
	_, _ = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, event.Data)
}
