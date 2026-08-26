package ttl

import (
	"context"
	"sync"
	"time"
)

type Kind string

const (
	MarkDelayed Kind = "delayed"
	ExpireLease Kind = "expire"
	Collect     Kind = "gc"
)

type Task struct {
	Kind            Kind
	Service         string
	InstanceID      string
	LeaseID         string
	ExpectedVersion string
	Deadline        time.Time
}

type item struct {
	task   Task
	rounds int64
}

// Wheel is a fixed-size timing wheel. Callbacks are always executed after its
// lock is released, so callbacks may safely call the Registry.
type Wheel struct {
	mu       sync.Mutex
	tick     time.Duration
	slots    [][]item
	position int
	current  time.Time
	now      func() time.Time
	handler  func([]Task)
}

func New(size int, tick time.Duration, now func() time.Time, handler func([]Task)) *Wheel {
	if size < 2 {
		size = 2
	}
	if tick <= 0 {
		tick = time.Second
	}
	if now == nil {
		now = time.Now
	}
	current := now().Truncate(tick)
	return &Wheel{
		tick:    tick,
		slots:   make([][]item, size),
		current: current,
		now:     now,
		handler: handler,
	}
}

func (w *Wheel) Schedule(task Task) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ticks := int64((task.Deadline.Sub(w.current) + w.tick - 1) / w.tick)
	if ticks < 1 {
		ticks = 1
	}
	slot := (w.position + int(ticks%int64(len(w.slots)))) % len(w.slots)
	rounds := (ticks - 1) / int64(len(w.slots))
	w.slots[slot] = append(w.slots[slot], item{task: task, rounds: rounds})
}

// Advance processes all complete ticks through target. It is exported to make
// state-machine tests deterministic without sleeping.
func (w *Wheel) Advance(target time.Time) int {
	processed := 0
	for {
		w.mu.Lock()
		if w.current.Add(w.tick).After(target) {
			w.mu.Unlock()
			return processed
		}
		w.current = w.current.Add(w.tick)
		w.position = (w.position + 1) % len(w.slots)
		items := w.slots[w.position]
		w.slots[w.position] = nil
		due := make([]Task, 0, len(items))
		for _, scheduled := range items {
			if scheduled.rounds > 0 {
				scheduled.rounds--
				w.slots[w.position] = append(w.slots[w.position], scheduled)
				continue
			}
			if scheduled.task.Deadline.After(w.current) {
				w.ScheduleLocked(scheduled.task)
				continue
			}
			due = append(due, scheduled.task)
		}
		w.mu.Unlock()
		if len(due) > 0 && w.handler != nil {
			w.handler(due)
		}
		processed += len(due)
	}
}

func (w *Wheel) ScheduleLocked(task Task) {
	ticks := int64((task.Deadline.Sub(w.current) + w.tick - 1) / w.tick)
	if ticks < 1 {
		ticks = 1
	}
	slot := (w.position + int(ticks%int64(len(w.slots)))) % len(w.slots)
	rounds := (ticks - 1) / int64(len(w.slots))
	w.slots[slot] = append(w.slots[slot], item{task: task, rounds: rounds})
}

func (w *Wheel) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			w.Advance(now)
		}
	}
}

func (w *Wheel) Pending() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	total := 0
	for _, slot := range w.slots {
		total += len(slot)
	}
	return total
}
