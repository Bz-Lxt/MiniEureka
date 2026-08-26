package events

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrCursorExpired = errors.New("event cursor expired")

type subscription struct {
	channel chan Event
	closed  bool
}

// Ring is a bounded event log and a non-blocking fan-out hub. Registry writes
// never wait for a WebSocket consumer.
type Ring struct {
	mu       sync.RWMutex
	entries  []Event
	capacity int
	nextSeq  uint64
	nodeID   string
	bootID   string
	nextSub  uint64
	subs     map[uint64]*subscription
	dropped  uint64
}

func New(capacity int, nodeID, bootID string) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{
		entries:  make([]Event, 0, capacity),
		capacity: capacity,
		nextSeq:  1,
		nodeID:   nodeID,
		bootID:   bootID,
		subs:     make(map[uint64]*subscription),
	}
}

func (r *Ring) Publish(event Event) Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.Seq = r.nextSeq
	r.nextSeq++
	event.SchemaVersion = SchemaVersion
	event.StreamNodeID = r.nodeID
	event.StreamBootID = r.bootID
	if event.OriginNodeID == "" {
		event.OriginNodeID = r.nodeID
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.EventID == "" {
		event.EventID = fmt.Sprintf("evt-%s-%d", r.nodeID, event.Seq)
	}
	event = cloneEvent(event)
	if len(r.entries) == r.capacity {
		copy(r.entries, r.entries[1:])
		r.entries[len(r.entries)-1] = event
	} else {
		r.entries = append(r.entries, event)
	}
	for id, sub := range r.subs {
		select {
		case sub.channel <- cloneEvent(event):
		default:
			r.dropped++
			for len(sub.channel) > 0 {
				<-sub.channel
			}
			sub.channel <- Event{
				Seq:           event.Seq,
				SchemaVersion: SchemaVersion,
				StreamNodeID:  r.nodeID,
				StreamBootID:  r.bootID,
				Type:          ResyncRequired,
				EventID:       fmt.Sprintf("resync-%s-%d", r.nodeID, event.Seq),
				OriginNodeID:  r.nodeID,
				OccurredAt:    time.Now().UTC(),
				Payload:       jsonObject("slow_consumer"),
			}
			close(sub.channel)
			sub.closed = true
			delete(r.subs, id)
		}
	}
	return cloneEvent(event)
}

func (r *Ring) Since(cursor uint64, limit int) ([]Event, uint64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 {
		limit = 200
	}
	if len(r.entries) == 0 {
		return []Event{}, r.nextSeq - 1, nil
	}
	oldest := r.entries[0].Seq
	if cursor > 0 && cursor < oldest-1 {
		return nil, r.nextSeq - 1, ErrCursorExpired
	}
	result := make([]Event, 0, min(limit, len(r.entries)))
	for _, entry := range r.entries {
		if entry.Seq <= cursor {
			continue
		}
		result = append(result, cloneEvent(entry))
		if len(result) == limit {
			break
		}
	}
	return result, r.nextSeq - 1, nil
}

func (r *Ring) Subscribe(cursor uint64, queue int) (<-chan Event, func(), error) {
	if queue < 2 {
		queue = 2
	}
	r.mu.Lock()
	if len(r.entries) > 0 && cursor > 0 && cursor < r.entries[0].Seq-1 {
		r.mu.Unlock()
		return nil, nil, ErrCursorExpired
	}
	pendingEntries := make([]Event, 0)
	for _, entry := range r.entries {
		if entry.Seq > cursor {
			pendingEntries = append(pendingEntries, entry)
		}
	}
	r.nextSub++
	id := r.nextSub
	r.mu.Unlock()

	pending := make([]Event, 0, len(pendingEntries))
	for _, entry := range pendingEntries {
		pending = append(pending, cloneEvent(entry))
	}
	if queue < len(pending)+1 {
		queue = len(pending) + 1
	}
	sub := &subscription{channel: make(chan Event, queue)}
	for _, event := range pending {
		sub.channel <- event
	}
	r.mu.Lock()
	r.subs[id] = sub
	r.mu.Unlock()
	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		current, ok := r.subs[id]
		if !ok || current.closed {
			return
		}
		current.closed = true
		delete(r.subs, id)
		close(current.channel)
	}
	return sub.channel, cancel, nil
}

func (r *Ring) Cursor() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.nextSeq - 1
}

func (r *Ring) Dropped() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dropped
}

func (r *Ring) Recent(limit int) []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > len(r.entries) {
		limit = len(r.entries)
	}
	start := len(r.entries) - limit
	result := make([]Event, 0, limit)
	for _, event := range r.entries[start:] {
		result = append(result, cloneEvent(event))
	}
	return result
}

func jsonObject(reason string) []byte {
	return []byte(fmt.Sprintf(`{"reason":%q}`, reason))
}
