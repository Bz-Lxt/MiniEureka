package cluster

import (
	"sort"
	"sync"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/model"
)

type ChangeHook func(model.Member)

type peerState struct {
	member  model.Member
	lastOK  *time.Time
	latency time.Duration
}

type Table struct {
	mu        sync.RWMutex
	selfID    string
	clock     *clock.HLC
	suspicion time.Duration
	dead      time.Duration
	members   map[string]peerState
	hook      ChangeHook
}

func NewTable(self model.Member, hlc *clock.HLC, suspicion, dead time.Duration, hook ChangeHook) *Table {
	if suspicion <= 0 {
		suspicion = 5 * time.Second
	}
	if dead <= suspicion {
		dead = 15 * time.Second
	}
	return &Table{
		selfID:    self.NodeID,
		clock:     hlc,
		suspicion: suspicion,
		dead:      dead,
		members:   map[string]peerState{self.NodeID: {member: cloneMember(self)}},
		hook:      hook,
	}
}

// Merge applies the deterministic member order incarnation, version, status.
// It returns the effective value and whether the table changed.
func (t *Table) Merge(incoming model.Member) (model.Member, bool) {
	if incoming.NodeID == "" || incoming.BootID == "" || !incoming.Status.Valid() || incoming.Incarnation == 0 || incoming.Version.IsZero() {
		return model.Member{}, false
	}
	if incoming.NodeID == t.selfID && incoming.Status != model.MemberAlive {
		return t.refute(incoming)
	}
	t.mu.Lock()
	current, exists := t.members[incoming.NodeID]
	if exists && compareMember(incoming, current.member) <= 0 {
		result := cloneMember(current.member)
		t.mu.Unlock()
		return result, false
	}
	next := peerState{member: cloneMember(incoming)}
	if exists {
		next.lastOK, next.latency = current.lastOK, current.latency
	}
	if incoming.Status == model.MemberAlive {
		seen := incoming.LastSeenAt
		next.lastOK = &seen
	}
	t.members[incoming.NodeID] = next
	result := cloneMember(next.member)
	t.mu.Unlock()
	t.notify(result)
	return result, true
}

func (t *Table) refute(incoming model.Member) (model.Member, bool) {
	t.mu.Lock()
	self := t.members[t.selfID]
	if incoming.Incarnation < self.member.Incarnation {
		result := cloneMember(self.member)
		t.mu.Unlock()
		return result, false
	}
	self.member.Incarnation = incoming.Incarnation + 1
	self.member.Status = model.MemberAlive
	self.member.Version = t.clock.Now()
	self.member.LastSeenAt = time.Now().UTC()
	seen := self.member.LastSeenAt
	self.lastOK = &seen
	t.members[t.selfID] = self
	result := cloneMember(self.member)
	t.mu.Unlock()
	t.notify(result)
	return result, true
}

func (t *Table) ObserveAlive(member model.Member, latency time.Duration, now time.Time) (model.Member, bool) {
	member.Status = model.MemberAlive
	member.LastSeenAt = now.UTC()
	effective, changed := t.Merge(member)
	t.mu.Lock()
	state, ok := t.members[member.NodeID]
	if ok && effective.Status == model.MemberAlive {
		seen := now.UTC()
		state.lastOK = &seen
		state.latency = latency
		state.member.LastSeenAt = seen
		t.members[member.NodeID] = state
	}
	t.mu.Unlock()
	return effective, changed
}

// Tick advances the local failure detector. It does not wait for quorum.
func (t *Table) Tick(now time.Time) []model.Member {
	changed := make([]model.Member, 0)
	t.mu.Lock()
	for nodeID, state := range t.members {
		if nodeID == t.selfID || state.member.Status == model.MemberDead {
			continue
		}
		age := now.Sub(state.member.LastSeenAt)
		next := state.member.Status
		switch {
		case age >= t.dead:
			next = model.MemberDead
		case age >= t.suspicion && state.member.Status == model.MemberAlive:
			next = model.MemberSuspect
		}
		if next == state.member.Status {
			continue
		}
		state.member.Status = next
		state.member.Version = t.clock.Now()
		state.member.Version.OriginNodeID = t.selfID
		t.members[nodeID] = state
		changed = append(changed, cloneMember(state.member))
	}
	t.mu.Unlock()
	for _, member := range changed {
		t.notify(member)
	}
	return changed
}

func (t *Table) Get(nodeID string) (model.Member, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	state, ok := t.members[nodeID]
	return cloneMember(state.member), ok
}

func (t *Table) Snapshot() []model.Member {
	t.mu.RLock()
	result := make([]model.Member, 0, len(t.members))
	for _, state := range t.members {
		result = append(result, cloneMember(state.member))
	}
	t.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].NodeID < result[j].NodeID })
	return result
}

func (t *Table) AlivePeers() []model.Member {
	members := t.Snapshot()
	result := make([]model.Member, 0, len(members))
	for _, member := range members {
		if member.NodeID != t.selfID && member.Status == model.MemberAlive {
			result = append(result, member)
		}
	}
	return result
}

func (t *Table) Edges() []model.TopologyEdge {
	t.mu.RLock()
	result := make([]model.TopologyEdge, 0, len(t.members)-1)
	for nodeID, state := range t.members {
		if nodeID == t.selfID {
			continue
		}
		result = append(result, model.TopologyEdge{
			SourceNodeID: t.selfID,
			TargetNodeID: nodeID,
			State:        string(state.member.Status),
			LastSuccess:  cloneTime(state.lastOK),
			LatencyMS:    state.latency.Milliseconds(),
		})
	}
	t.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].TargetNodeID < result[j].TargetNodeID })
	return result
}

func (t *Table) Self() model.Member {
	member, _ := t.Get(t.selfID)
	return member
}

func (t *Table) notify(member model.Member) {
	if t.hook != nil {
		t.hook(cloneMember(member))
	}
}

func compareMember(a, b model.Member) int {
	if a.Incarnation < b.Incarnation {
		return -1
	}
	if a.Incarnation > b.Incarnation {
		return 1
	}
	if comparison := model.CompareVersion(a.Version, b.Version); comparison != 0 {
		return comparison
	}
	if a.Status.Priority() < b.Status.Priority() {
		return -1
	}
	if a.Status.Priority() > b.Status.Priority() {
		return 1
	}
	return 0
}

func cloneMember(member model.Member) model.Member { return member }

func cloneTime(value *time.Time) *time.Time {
	result := new(time.Time)
	*result = *value
	return result
}
