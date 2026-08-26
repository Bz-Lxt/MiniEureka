package registry

import (
	"errors"
	"hash/fnv"
	"sync"
	"time"

	"minieureka/internal/model"
)

const (
	DefaultShards          = 64
	defaultOperationWindow = 32
)

var ErrInvalidShardCount = errors.New("registry shard count must be a power of two")

type Registry struct {
	shards []shard
	mask   uint64
	now    func() time.Time
}

type shard struct {
	mu       sync.RWMutex
	services map[string]map[string]*entry
	fences   map[model.Key]Fence
	revision uint64
}

type entry struct {
	record         model.Instance
	kind           model.MutationKind
	eventID        string
	operations     map[string]string
	operationOrder []string
}

type Fence struct {
	Key        model.Key          `json:"key"`
	LeaseEpoch model.Version      `json:"lease_epoch"`
	Version    model.Version      `json:"version"`
	Kind       model.MutationKind `json:"kind"`
	EventID    string             `json:"event_id"`
	Generation uint64             `json:"generation"`
}

type Option func(*Registry)

// WithNow is primarily useful to make local TTL deadline construction
// deterministic. It must be configured before concurrent use begins.
func WithNow(now func() time.Time) Option {
	return func(r *Registry) {
		if now != nil {
			r.now = now
		}
	}
}

func New(shardCount int, options ...Option) (*Registry, error) {
	if shardCount <= 0 || shardCount&(shardCount-1) != 0 {
		return nil, ErrInvalidShardCount
	}
	r := &Registry{
		shards: make([]shard, shardCount),
		mask:   uint64(shardCount - 1),
		now:    time.Now,
	}
	for i := range r.shards {
		r.shards[i].services = make(map[string]map[string]*entry)
		r.shards[i].fences = make(map[model.Key]Fence)
	}
	for _, option := range options {
		option(r)
	}
	return r, nil
}

func (r *Registry) ShardCount() int { return len(r.shards) }

func (r *Registry) ShardIndex(service string) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(service))
	return int(h.Sum64() & r.mask)
}

func (r *Registry) shardFor(service string) *shard {
	return &r.shards[r.ShardIndex(service)]
}

func (e *entry) cloneRecord() model.Instance { return e.record.Clone() }

func (e *entry) operationEvent(operationID string) (string, bool) {
	if operationID == "" || e.operations == nil {
		return "", false
	}
	eventID, ok := e.operations[operationID]
	return eventID, ok
}

func (e *entry) rememberOperation(operationID, eventID string) {
	if operationID == "" {
		return
	}
	if e.operations == nil {
		e.operations = make(map[string]string, defaultOperationWindow)
	}
	if _, exists := e.operations[operationID]; exists {
		return
	}
	if len(e.operationOrder) == defaultOperationWindow {
		delete(e.operations, e.operationOrder[0])
		copy(e.operationOrder, e.operationOrder[1:])
		e.operationOrder[len(e.operationOrder)-1] = operationID
	} else {
		e.operationOrder = append(e.operationOrder, operationID)
	}
	e.operations[operationID] = eventID
}
