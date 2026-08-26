package service

import (
	"time"

	"minieureka/internal/events"
	"minieureka/internal/model"
	"minieureka/internal/registry"
)

type RegisterRequest struct {
	Service        string
	InstanceID     string
	RegistrationID string
	Host           string
	Port           int
	Protocol       model.Protocol
	Metadata       map[string]string
	Demo           bool
}

type OperationResult struct {
	Record    model.Instance
	EventID   string
	Duplicate bool
}

func (s *Service) Register(request RegisterRequest) (OperationResult, error) {
	now := s.opts.Now().UTC()
	generation := uint64(1)
	if current, ok := s.registry.Get(request.Service, request.InstanceID); ok {
		if current.RegistrationID == request.RegistrationID {
			eventID, _ := s.registry.CurrentEventID(request.Service, request.InstanceID)
			return OperationResult{Record: current, EventID: eventID, Duplicate: true}, nil
		}
		generation = current.Generation + 1
	} else if fence, ok := s.registry.GetFence(model.Key{Service: request.Service, InstanceID: request.InstanceID}); ok {
		generation = fence.Generation + 1
	}
	epoch := s.clock.Now()
	record := model.Instance{
		Service: request.Service, InstanceID: request.InstanceID, RegistrationID: request.RegistrationID,
		Host: request.Host, Port: request.Port, Protocol: request.Protocol, Metadata: request.Metadata,
		Status: model.StatusActive, StatusReason: model.ReasonRegistered, Generation: generation,
		LeaseID: newID("lease"), LeaseEpoch: epoch, Version: epoch, OriginNodeID: s.opts.NodeID,
		RegisteredAt: now, LastHeartbeatAt: now, UpdatedAt: now, Demo: request.Demo,
	}
	mutation := model.Mutation{Kind: model.MutationRegister, Record: record, EventID: newID("evt"), RemainingTTLMillis: s.opts.LeaseTTL.Milliseconds()}
	result, event, err := s.apply(mutation, true)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{Record: result.Record, EventID: appliedEventID(result, event), Duplicate: result.Duplicate}, nil
}

func (s *Service) Heartbeat(serviceName, instanceID, leaseID, operationID string) (OperationResult, error) {
	current, ok := s.registry.Get(serviceName, instanceID)
	if !ok {
		return OperationResult{}, ErrNotFound
	}
	if current.LeaseID != leaseID || current.ExplicitlyTerminal() {
		return OperationResult{}, ErrStaleLease
	}
	now := s.opts.Now().UTC()
	current.Status = model.StatusActive
	current.StatusReason = model.ReasonHeartbeatOK
	current.Version = s.clock.Now()
	current.OriginNodeID = s.opts.NodeID
	current.LastHeartbeatAt = now
	current.UpdatedAt = now
	current.EvictedAt = nil
	mutation := model.Mutation{Kind: model.MutationHeartbeat, Record: current, EventID: newID("evt"), OperationID: operationID, RemainingTTLMillis: s.opts.LeaseTTL.Milliseconds()}
	result, event, err := s.apply(mutation, true)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{Record: result.Record, EventID: appliedEventID(result, event), Duplicate: result.Duplicate}, nil
}

func (s *Service) Deregister(serviceName, instanceID, leaseID, operationID string, demo bool) (OperationResult, error) {
	current, ok := s.registry.Get(serviceName, instanceID)
	if !ok {
		return OperationResult{}, ErrNotFound
	}
	if current.LeaseID != leaseID {
		return OperationResult{}, ErrStaleLease
	}
	if demo && !current.Demo {
		return OperationResult{}, ErrNotDemo
	}
	now := s.opts.Now().UTC()
	evictedAt := now
	current.Status = model.StatusEvicted
	kind := model.MutationDeregister
	current.StatusReason = model.ReasonDeregistered
	if demo {
		kind = model.MutationDemoOffline
		current.StatusReason = model.ReasonDemoOffline
	}
	current.Version = s.clock.Now()
	current.OriginNodeID = s.opts.NodeID
	current.UpdatedAt = now
	current.EvictedAt = &evictedAt
	current.LeaseDeadline = time.Time{}
	mutation := model.Mutation{Kind: kind, Record: current, EventID: newID("evt"), OperationID: operationID}
	result, event, err := s.apply(mutation, true)
	if err != nil {
		if eventID, ok := s.registry.CurrentEventID(serviceName, instanceID); ok {
			return OperationResult{Record: current, EventID: eventID, Duplicate: true}, nil
		}
		return OperationResult{}, err
	}
	return OperationResult{Record: result.Record, EventID: appliedEventID(result, event), Duplicate: result.Duplicate}, nil
}

func appliedEventID(result registry.ApplyResult, event events.Event) string {
	if result.Duplicate {
		return result.EventID
	}
	return event.EventID
}

func (s *Service) Discover(serviceName string) []model.Instance {
	return s.registry.Discover(serviceName)
}
func (s *Service) Services() []registry.ServiceSummary { return s.registry.Services() }

func (s *Service) DashboardInstances() []model.Instance {
	now := s.opts.Now()
	all := s.registry.Snapshot()
	result := make([]model.Instance, 0, len(all))
	for _, record := range all {
		if record.Status == model.StatusEvicted && record.EvictedAt != nil && now.Sub(*record.EvictedAt) > s.opts.EvictedDisplayTTL {
			continue
		}
		result = append(result, record)
	}
	return result
}

func (s *Service) publishMutation(mutation model.Mutation, record model.Instance) events.Event {
	eventType := events.InstanceHeartbeat
	switch mutation.Kind {
	case model.MutationRegister:
		eventType = events.InstanceRegistered
	case model.MutationTTLExpire, model.MutationDeregister, model.MutationDemoOffline:
		eventType = events.InstanceEvicted
	}
	return s.events.Publish(events.Event{
		Type: eventType, EventID: mutation.EventID, EntityKey: record.Key().String(),
		Revision: record.Version.String(), OriginNodeID: record.OriginNodeID,
		OccurredAt: record.UpdatedAt, Payload: events.Payload(map[string]any{"instance": record}),
	})
}
