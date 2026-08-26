package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"minieureka/internal/model"
)

type ApplyResult struct {
	Record    model.Instance
	Previous  *model.Instance
	EventID   string
	Applied   bool
	Duplicate bool
	Stale     bool
}

func (r *Registry) Apply(mutation model.Mutation) (ApplyResult, error) {
	if err := mutation.Validate(); err != nil {
		return ApplyResult{}, fmt.Errorf("apply mutation: %w", err)
	}

	key := mutation.Record.Key()
	s := r.shardFor(key.Service)
	s.mu.Lock()
	defer s.mu.Unlock()

	instances := s.services[key.Service]
	current := instances[key.InstanceID]
	if current == nil {
		if fence, ok := s.fences[key]; ok && !incomingBeatsFence(mutation, fence) {
			return ApplyResult{Stale: true}, nil
		}
		incoming := prepareIncoming(mutation, r.now())
		if instances == nil {
			instances = make(map[string]*entry)
			s.services[key.Service] = instances
		}
		created := &entry{record: incoming, kind: mutation.Kind, eventID: mutation.EventID}
		created.rememberOperation(mutation.OperationID, mutation.EventID)
		instances[key.InstanceID] = created
		updateFence(s, mutation)
		s.revision++
		return ApplyResult{Record: incoming.Clone(), EventID: mutation.EventID, Applied: true}, nil
	}

	currentRecord := current.cloneRecord()
	if eventID, seen := current.operationEvent(mutation.OperationID); seen {
		return ApplyResult{Record: currentRecord, EventID: eventID, Duplicate: true}, nil
	}

	decision := compareIncoming(mutation, current)
	if decision == 0 {
		current.rememberOperation(mutation.OperationID, current.eventID)
		return ApplyResult{Record: currentRecord, EventID: current.eventID, Duplicate: true}, nil
	}
	if decision < 0 {
		return ApplyResult{Record: currentRecord, Stale: true}, nil
	}

	incoming := prepareIncoming(mutation, r.now())
	previous := currentRecord
	operations := current.operations
	operationOrder := current.operationOrder
	if model.CompareVersion(incoming.LeaseEpoch, current.record.LeaseEpoch) > 0 {
		// Operation IDs are scoped to one lease. Retaining the old set could make a
		// new client operation accidentally look like an old retry.
		operations = nil
		operationOrder = nil
	}
	replacement := &entry{
		record:         incoming,
		kind:           mutation.Kind,
		eventID:        mutation.EventID,
		operations:     operations,
		operationOrder: operationOrder,
	}
	replacement.rememberOperation(mutation.OperationID, mutation.EventID)
	instances[key.InstanceID] = replacement
	updateFence(s, mutation)
	s.revision++
	return ApplyResult{Record: incoming.Clone(), Previous: &previous, EventID: mutation.EventID, Applied: true}, nil
}

// compareIncoming returns 1 when incoming wins, 0 for an exact/idempotent
// duplicate, and -1 when incoming is stale or forbidden by a terminal lease.
func compareIncoming(incoming model.Mutation, current *entry) int {
	epochComparison := model.CompareVersion(incoming.Record.LeaseEpoch, current.record.LeaseEpoch)
	if epochComparison != 0 {
		return epochComparison
	}

	if current.record.ExplicitlyTerminal() && !incoming.Kind.ExplicitlyTerminal() {
		return -1
	}
	if incoming.Kind.ExplicitlyTerminal() && !current.record.ExplicitlyTerminal() {
		return 1
	}
	if incoming.Record.LeaseID != current.record.LeaseID {
		// Equal lease epochs represent one lease identity. A different lease ID is
		// malformed state, not authority to replace the established lease.
		return -1
	}

	versionComparison := model.CompareVersion(incoming.Record.Version, current.record.Version)
	if versionComparison != 0 {
		return versionComparison
	}
	if incoming.Kind == current.kind && incoming.EventID == current.eventID &&
		incoming.Record.Equal(current.record) {
		return 0
	}
	if incoming.Kind.Priority() != current.kind.Priority() {
		if incoming.Kind.Priority() > current.kind.Priority() {
			return 1
		}
		return -1
	}
	if comparison := strings.Compare(incoming.EventID, current.eventID); comparison != 0 {
		return comparison
	}

	// Same event IDs should be byte-for-byte identical. This final canonical
	// comparison makes convergence deterministic even for malformed peers.
	incomingBytes, _ := json.Marshal(incoming.Record)
	currentBytes, _ := json.Marshal(current.record)
	return bytes.Compare(incomingBytes, currentBytes)
}

func incomingBeatsFence(incoming model.Mutation, fence Fence) bool {
	epochComparison := model.CompareVersion(incoming.Record.LeaseEpoch, fence.LeaseEpoch)
	if epochComparison != 0 {
		return epochComparison > 0
	}
	if fence.Kind.ExplicitlyTerminal() && !incoming.Kind.ExplicitlyTerminal() {
		return false
	}
	if incoming.Kind.ExplicitlyTerminal() && !fence.Kind.ExplicitlyTerminal() {
		return true
	}
	versionComparison := model.CompareVersion(incoming.Record.Version, fence.Version)
	if versionComparison != 0 {
		return versionComparison > 0
	}
	if incoming.Kind.Priority() != fence.Kind.Priority() {
		return incoming.Kind.Priority() > fence.Kind.Priority()
	}
	return strings.Compare(incoming.EventID, fence.EventID) > 0
}

func prepareIncoming(mutation model.Mutation, now time.Time) model.Instance {
	incoming := mutation.Record.Clone()
	incoming.LastRemainingTTLMs = mutation.RemainingTTLMillis
	switch mutation.Kind {
	case model.MutationRegister, model.MutationHeartbeat:
		if mutation.RemainingTTLMillis > 0 {
			incoming.LeaseDeadline = now.Add(time.Duration(mutation.RemainingTTLMillis) * time.Millisecond)
		} else {
			incoming.LeaseDeadline = time.Time{}
		}
	case model.MutationDelayed:
		// A DELAYED projection is normally applied through MarkDelayed. Preserve a
		// supplied local deadline for callers reconstructing a local snapshot.
	default:
		incoming.LeaseDeadline = time.Time{}
	}
	return incoming
}

func updateFence(s *shard, mutation model.Mutation) {
	if mutation.Record.Status != model.StatusEvicted {
		return
	}
	s.fences[mutation.Record.Key()] = Fence{
		Key:        mutation.Record.Key(),
		LeaseEpoch: mutation.Record.LeaseEpoch,
		Version:    mutation.Record.Version,
		Kind:       mutation.Kind,
		EventID:    mutation.EventID,
		Generation: mutation.Record.Generation,
	}
}

// ApplyFence merges a compact tombstone received after its full record was
// collected. A winning fence also removes any retained state it dominates.
func (r *Registry) ApplyFence(fence Fence) bool {
	if fence.Key.Validate() != nil || fence.LeaseEpoch.Validate() != nil ||
		fence.Version.Validate() != nil || fence.EventID == "" ||
		fence.Generation == 0 ||
		(fence.Kind != model.MutationTTLExpire && !fence.Kind.ExplicitlyTerminal()) {
		return false
	}

	s := r.shardFor(fence.Key.Service)
	s.mu.Lock()
	defer s.mu.Unlock()

	if instances := s.services[fence.Key.Service]; instances != nil {
		if current := instances[fence.Key.InstanceID]; current != nil {
			if compareFenceToEntry(fence, current) <= 0 {
				return false
			}
			delete(instances, fence.Key.InstanceID)
			if len(instances) == 0 {
				delete(s.services, fence.Key.Service)
			}
		}
	}
	if current, ok := s.fences[fence.Key]; ok && compareFences(fence, current) <= 0 {
		return false
	}
	s.fences[fence.Key] = fence
	s.revision++
	return true
}

func compareFenceToEntry(fence Fence, current *entry) int {
	if comparison := model.CompareVersion(fence.LeaseEpoch, current.record.LeaseEpoch); comparison != 0 {
		return comparison
	}
	if current.record.ExplicitlyTerminal() && !fence.Kind.ExplicitlyTerminal() {
		return -1
	}
	if fence.Kind.ExplicitlyTerminal() && !current.record.ExplicitlyTerminal() {
		return 1
	}
	if comparison := model.CompareVersion(fence.Version, current.record.Version); comparison != 0 {
		return comparison
	}
	if fence.Kind.Priority() != current.kind.Priority() {
		if fence.Kind.Priority() > current.kind.Priority() {
			return 1
		}
		return -1
	}
	return strings.Compare(fence.EventID, current.eventID)
}

func compareFences(incoming, current Fence) int {
	if comparison := model.CompareVersion(incoming.LeaseEpoch, current.LeaseEpoch); comparison != 0 {
		return comparison
	}
	if current.Kind.ExplicitlyTerminal() && !incoming.Kind.ExplicitlyTerminal() {
		return -1
	}
	if incoming.Kind.ExplicitlyTerminal() && !current.Kind.ExplicitlyTerminal() {
		return 1
	}
	if comparison := model.CompareVersion(incoming.Version, current.Version); comparison != 0 {
		return comparison
	}
	if incoming.Kind.Priority() != current.Kind.Priority() {
		if incoming.Kind.Priority() > current.Kind.Priority() {
			return 1
		}
		return -1
	}
	return strings.Compare(incoming.EventID, current.EventID)
}
