package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/events"
	"minieureka/internal/model"
	"minieureka/internal/registry"
	"minieureka/internal/ttl"
)

var (
	ErrNotFound   = errors.New("instance not found")
	ErrStaleLease = errors.New("stale lease")
	ErrNotDemo    = errors.New("instance is not a demo instance")
)

type Options struct {
	NodeID            string
	LeaseTTL          time.Duration
	DelayedAfter      time.Duration
	TombstoneTTL      time.Duration
	EvictedDisplayTTL time.Duration
	Now               func() time.Time
	OnMutation        func(model.Mutation)
}

type Service struct {
	registry   *registry.Registry
	clock      *clock.HLC
	events     *events.Ring
	wheel      *ttl.Wheel
	opts       Options
	mu         sync.RWMutex
	onMutation func(model.Mutation)
}

func New(store *registry.Registry, hlc *clock.HLC, eventRing *events.Ring, wheel *ttl.Wheel, options Options) *Service {
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 30 * time.Second
	}
	if options.DelayedAfter <= 0 {
		options.DelayedAfter = options.LeaseTTL / 2
	}
	if options.TombstoneTTL <= 0 {
		options.TombstoneTTL = 5 * time.Minute
	}
	if options.EvictedDisplayTTL <= 0 {
		options.EvictedDisplayTTL = time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{registry: store, clock: hlc, events: eventRing, wheel: wheel, opts: options, onMutation: options.OnMutation}
}

func (s *Service) SetMutationSink(sink func(model.Mutation)) {
	s.mu.Lock()
	s.onMutation = sink
	s.mu.Unlock()
}

func (s *Service) Registry() *registry.Registry { return s.registry }
func (s *Service) Events() *events.Ring         { return s.events }

func (s *Service) apply(mutation model.Mutation, local bool) (registry.ApplyResult, events.Event, error) {
	result, err := s.registry.Apply(mutation)
	if err != nil {
		return registry.ApplyResult{}, events.Event{}, err
	}
	if !result.Applied {
		return result, events.Event{}, nil
	}
	event := s.publishMutation(mutation, result.Record)
	s.schedule(result.Record)
	s.mu.RLock()
	sink := s.onMutation
	s.mu.RUnlock()
	if sink != nil {
		// Forward both local and newly applied remote state. Comparator-based
		// idempotency, rather than a finite dedupe cache, stops loops.
		sink(mutation)
	}
	_ = local
	return result, event, nil
}

func (s *Service) ApplyRemote(mutation model.Mutation) (registry.ApplyResult, error) {
	if _, err := s.clock.Observe(mutation.Record.Version); err != nil {
		return registry.ApplyResult{}, err
	}
	result, _, err := s.apply(mutation, false)
	return result, err
}

func (s *Service) schedule(record model.Instance) {
	if s.wheel == nil {
		return
	}
	if record.Discoverable() && !record.LeaseDeadline.IsZero() {
		delayedAt := record.LeaseDeadline.Add(-(s.opts.LeaseTTL - s.opts.DelayedAfter))
		s.wheel.Schedule(ttl.Task{Kind: ttl.MarkDelayed, Service: record.Service, InstanceID: record.InstanceID, LeaseID: record.LeaseID, ExpectedVersion: record.Version.String(), Deadline: delayedAt})
		s.wheel.Schedule(ttl.Task{Kind: ttl.ExpireLease, Service: record.Service, InstanceID: record.InstanceID, LeaseID: record.LeaseID, ExpectedVersion: record.Version.String(), Deadline: record.LeaseDeadline})
	}
}

func (s *Service) HandleTasks(tasks []ttl.Task) {
	for _, task := range tasks {
		s.handleTask(task)
	}
}

func (s *Service) handleTask(task ttl.Task) {
	now := s.opts.Now().UTC()
	if task.Kind == ttl.Collect {
		for _, key := range s.registry.GC(now, s.opts.TombstoneTTL) {
			s.events.Publish(events.Event{Type: events.InstanceRemoved, EntityKey: key.String(), Payload: events.Payload(map[string]any{"service": key.Service, "instance_id": key.InstanceID})})
		}
		return
	}
	version, err := model.ParseVersion(task.ExpectedVersion)
	if err != nil {
		return
	}
	key := model.Key{Service: task.Service, InstanceID: task.InstanceID}
	switch task.Kind {
	case ttl.MarkDelayed:
		if record, ok := s.registry.MarkDelayed(key, task.LeaseID, version, now); ok {
			s.events.Publish(events.Event{Type: events.InstanceDelayed, EntityKey: key.String(), Revision: record.Version.String(), Payload: events.Payload(map[string]any{"instance": record})})
		}
	case ttl.ExpireLease:
		record, ok := s.registry.Get(key.Service, key.InstanceID)
		if !ok || record.LeaseID != task.LeaseID || model.CompareVersion(record.Version, version) != 0 || !record.Discoverable() || record.LeaseDeadline.After(now) {
			return
		}
		evictedAt := now
		record.Status = model.StatusEvicted
		record.StatusReason = model.ReasonTTLExpired
		record.Version = s.clock.Now()
		record.OriginNodeID = s.opts.NodeID
		record.UpdatedAt = now
		record.EvictedAt = &evictedAt
		record.LeaseDeadline = time.Time{}
		mutation := model.Mutation{Kind: model.MutationTTLExpire, Record: record, EventID: newID("evt")}
		_, _, _ = s.apply(mutation, true)
	}
}

func newID(prefix string) string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(value[:])
}
