package events

import (
	"errors"
	"sync"
	"testing"
)

func TestRingCursorAndExpiry(t *testing.T) {
	t.Parallel()
	ring := New(3, "n1", "b1")
	for range 5 {
		ring.Publish(Event{Type: InstanceHeartbeat, Payload: Payload(map[string]bool{"ok": true})})
	}
	if _, _, err := ring.Since(1, 10); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("Since() error = %v, want cursor expired", err)
	}
	got, cursor, err := ring.Since(2, 10)
	if err != nil || len(got) != 3 || cursor != 5 {
		t.Fatalf("Since() = len %d, cursor %d, err %v", len(got), cursor, err)
	}
}

func TestRingConcurrentPublish(t *testing.T) {
	t.Parallel()
	ring := New(1024, "n1", "b1")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				ring.Publish(Event{Type: InstanceHeartbeat})
			}
		}()
	}
	wg.Wait()
	if ring.Cursor() != 800 {
		t.Fatalf("Cursor() = %d, want 800", ring.Cursor())
	}
}

func TestSlowSubscriberGetsResync(t *testing.T) {
	t.Parallel()
	ring := New(8, "n1", "b1")
	stream, cancel, err := ring.Subscribe(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	for range 3 {
		ring.Publish(Event{Type: InstanceHeartbeat})
	}
	event := <-stream
	if event.Type != ResyncRequired {
		t.Fatalf("event type = %s", event.Type)
	}
}
