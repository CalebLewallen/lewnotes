package events

import (
	"testing"
	"time"
)

func TestPublishAndReplay(t *testing.T) {
	hub := New(Options{Buffer: 4})
	first := hub.Publish("note.created", map[string]string{"id": "a"})
	second := hub.Publish("note.updated", map[string]string{"id": "a"})

	replay, ok, ch, cancel := hub.Subscribe(first.ID)
	defer cancel()
	if !ok {
		t.Fatal("Subscribe(existing id) ok = false")
	}
	if len(replay) != 1 || replay[0].ID != second.ID {
		t.Fatalf("replay = %+v, want second event", replay)
	}

	third := hub.Publish("note.deleted", map[string]string{"id": "a"})
	select {
	case got := <-ch:
		if got.ID != third.ID || got.Type != "note.deleted" {
			t.Errorf("live event = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live event")
	}
}

func TestReplayUnknownRequestsResync(t *testing.T) {
	hub := New(Options{Buffer: 2})
	hub.Publish("a", nil)
	hub.Publish("b", nil)
	hub.Publish("c", nil)

	_, ok, _, cancel := hub.Subscribe("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	defer cancel()
	if ok {
		t.Fatal("Subscribe(unknown id) ok = true, want resync")
	}
}

func TestReplayAfterWrap(t *testing.T) {
	hub := New(Options{Buffer: 3})
	first := hub.Publish("first", nil)
	hub.Publish("second", nil)
	third := hub.Publish("third", nil)
	hub.Publish("fourth", nil)

	replay, ok, _, cancel := hub.Subscribe(third.ID)
	defer cancel()
	if !ok {
		t.Fatal("Subscribe(known id) ok = false")
	}
	if len(replay) != 1 || replay[0].Type != "fourth" {
		// Ring is [fourth, second, third] with next pointing at second, so the
		// logical order is second, third, fourth.
		t.Fatalf("replay = %+v", replay)
	}
	if hub.Len() != 3 || first.ID == "" {
		t.Fatalf("Len = %d", hub.Len())
	}
}

func TestSlowSubscriberGetsResync(t *testing.T) {
	hub := New(Options{Buffer: 10})
	_, _, ch, cancel := hub.Subscribe("")
	defer cancel()

	// Fill the subscriber buffer, then publish once more so the hub marks it
	// as overflowed. Draining and publishing again must yield a resync.
	for i := 0; i < 64; i++ {
		hub.Publish("note.updated", map[string]int{"i": i})
	}
	hub.Publish("note.updated", map[string]int{"i": 64})
	for i := 0; i < 64; i++ {
		<-ch
	}
	hub.Publish("note.updated", map[string]int{"i": 65})

	select {
	case got := <-ch:
		if got.Type != "resync" {
			t.Fatalf("event type = %q, want resync", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resync")
	}
}
