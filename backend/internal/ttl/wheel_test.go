package ttl

import (
	"sync"
	"testing"
	"time"
)

func TestWheelFiresOnTickAndAcrossRounds(t *testing.T) {
	t.Parallel()
	start := time.Unix(1000, 0)
	var mu sync.Mutex
	var got []Task
	wheel := New(4, time.Second, func() time.Time { return start }, func(tasks []Task) {
		mu.Lock()
		got = append(got, tasks...)
		mu.Unlock()
	})
	wheel.Schedule(Task{InstanceID: "soon", Deadline: start.Add(1500 * time.Millisecond)})
	wheel.Schedule(Task{InstanceID: "later", Deadline: start.Add(6 * time.Second)})
	if processed := wheel.Advance(start.Add(time.Second)); processed != 0 {
		t.Fatalf("processed early = %d", processed)
	}
	wheel.Advance(start.Add(2 * time.Second))
	wheel.Advance(start.Add(6 * time.Second))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0].InstanceID != "soon" || got[1].InstanceID != "later" {
		t.Fatalf("got %#v", got)
	}
}

func TestWheelStaleItemsRemainIndependent(t *testing.T) {
	t.Parallel()
	start := time.Unix(1000, 0)
	var got []Task
	wheel := New(8, time.Second, func() time.Time { return start }, func(tasks []Task) { got = append(got, tasks...) })
	wheel.Schedule(Task{LeaseID: "old", Deadline: start.Add(time.Second)})
	wheel.Schedule(Task{LeaseID: "new", Deadline: start.Add(2 * time.Second)})
	wheel.Advance(start.Add(2 * time.Second))
	if len(got) != 2 {
		t.Fatalf("got %d tasks", len(got))
	}
}
