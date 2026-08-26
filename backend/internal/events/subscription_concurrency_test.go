package events_test

import (
	"bytes"
	"runtime"
	"testing"
	"time"

	"minieureka/internal/events"
)

func TestSubscribeDoesNotMissConcurrentPublish(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	const backlog = 4096
	ring := events.New(backlog+16, "node-1", "boot-1")
	payload := bytes.Repeat([]byte{'x'}, 16<<10)
	for range backlog {
		ring.Publish(events.Event{Type: events.InstanceHeartbeat, Payload: payload})
	}

	type subscription struct {
		stream <-chan events.Event
		cancel func()
		err    error
	}
	subscribed := make(chan subscription, 1)
	go func() {
		stream, cancel, err := ring.Subscribe(0, backlog+16)
		subscribed <- subscription{stream: stream, cancel: cancel, err: err}
	}()

	time.Sleep(2 * time.Millisecond)
	published := ring.Publish(events.Event{Type: events.InstanceRegistered})
	result := <-subscribed
	if result.err != nil {
		t.Fatalf("Subscribe() error = %v", result.err)
	}
	result.cancel()

	found := false
	for event := range result.stream {
		if event.Seq == published.Seq {
			found = true
		}
	}
	if !found {
		t.Fatalf("subscription missed concurrently published event seq %d", published.Seq)
	}
}
