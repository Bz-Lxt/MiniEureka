package gossip

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"minieureka/internal/cluster"
	"minieureka/internal/events"
	"minieureka/internal/model"
	"minieureka/internal/registry"
)

type MutationApplier interface {
	ApplyRemote(model.Mutation) (registry.ApplyResult, error)
}

type RegistryView interface {
	Digests() []registry.ShardDigest
	Digest(int) (registry.ShardDigest, bool)
	MutationsForShard(int) ([]model.Mutation, bool)
	Fences() []registry.Fence
	ShardIndex(string) int
	ApplyFence(registry.Fence) bool
}

type Counters interface {
	GossipSent()
	GossipReceived()
	GossipRejected()
}

type EngineConfig struct {
	NodeID              string
	AdvertiseAddress    string
	Seeds               []string
	Fanout              int
	Interval            time.Duration
	AntiEntropyInterval time.Duration
	PendingRounds       int
}

type traceMeta struct {
	hop           int
	originNodeID  string
	originAddress string
	trace         bool
}

type pendingMutation struct {
	mutation model.Mutation
	traceMeta
	rounds int
}

type Engine struct {
	config    EngineConfig
	auth      *Authenticator
	transport *UDPTransport
	selector  *Selector
	members   *cluster.Table
	registry  RegistryView
	applier   MutationApplier
	events    *events.Ring
	counters  Counters
	logger    *slog.Logger

	mu           sync.Mutex
	pending      map[string]pendingMutation
	incomingMeta map[string]traceMeta
	pings        map[string]time.Time
	syncing      map[string]bool
	digestCursor int
	memberCursor int
	ready        atomic.Bool
	wg           sync.WaitGroup
}

func NewEngine(config EngineConfig, auth *Authenticator, transport *UDPTransport, selector *Selector, members *cluster.Table, registryView RegistryView, applier MutationApplier, eventRing *events.Ring, counters Counters, logger *slog.Logger) *Engine {
	if config.Fanout <= 0 {
		config.Fanout = 3
	}
	if config.Interval <= 0 {
		config.Interval = time.Second
	}
	if config.AntiEntropyInterval <= 0 {
		config.AntiEntropyInterval = 10 * time.Second
	}
	if config.PendingRounds <= 0 {
		config.PendingRounds = 8
	}
	if selector == nil {
		selector = NewSelector(time.Now().UnixNano())
	}
	if logger == nil {
		logger = slog.Default()
	}
	engine := &Engine{
		config: config, auth: auth, transport: transport, selector: selector,
		members: members, registry: registryView, applier: applier, events: eventRing,
		counters: counters, logger: logger, pending: make(map[string]pendingMutation),
		incomingMeta: make(map[string]traceMeta), pings: make(map[string]time.Time), syncing: make(map[string]bool),
	}
	if len(config.Seeds) == 0 {
		engine.ready.Store(true)
	}
	return engine
}

func (e *Engine) Ready() bool { return e.ready.Load() }

func (e *Engine) Enqueue(mutation model.Mutation) {
	e.mu.Lock()
	meta, remote := e.incomingMeta[mutation.EventID]
	if remote {
		delete(e.incomingMeta, mutation.EventID)
	} else {
		meta = traceMeta{
			hop: 1, originNodeID: e.config.NodeID, originAddress: e.config.AdvertiseAddress,
			trace: mutation.Kind == model.MutationDemoOffline,
		}
	}
	if current, ok := e.pending[mutation.EventID]; ok {
		if meta.hop > 0 && meta.hop < current.hop {
			current.traceMeta = meta
		}
		current.rounds = e.config.PendingRounds
		e.pending[mutation.EventID] = current
		e.mu.Unlock()
		return
	}
	e.pending[mutation.EventID] = pendingMutation{mutation: mutation, traceMeta: meta, rounds: e.config.PendingRounds}
	e.mu.Unlock()
}

func (e *Engine) Run(ctx context.Context) error {
	listenErr := make(chan error, 1)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		listenErr <- e.transport.Listen(ctx, e.handleDatagram)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for e.transport.LocalAddr() == nil && time.Now().Before(deadline) {
		select {
		case err := <-listenErr:
			return err
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
	gossipTicker := time.NewTicker(e.config.Interval)
	antiTicker := time.NewTicker(e.config.AntiEntropyInterval)
	defer gossipTicker.Stop()
	defer antiTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = e.transport.Close()
			e.wg.Wait()
			return nil
		case err := <-listenErr:
			if err != nil && ctx.Err() == nil {
				return err
			}
			return nil
		case now := <-gossipTicker.C:
			e.members.Tick(now)
			e.gossipRound(ctx)
		case <-antiTicker.C:
			e.antiEntropyRound(ctx)
		}
	}
}

func (e *Engine) gossipRound(ctx context.Context) {
	targets := e.targets()
	selected := e.selector.Pick(targets, e.config.Fanout)
	for _, key := range selected {
		target := e.targetFor(key)
		if target.address == "" {
			continue
		}
		e.sendPing(ctx, target)
		e.sendMembers(ctx, target)
	}
	pending := e.pendingForRound()
	for _, item := range pending {
		for _, key := range selected {
			target := e.targetFor(key)
			if target.address == "" {
				continue
			}
			e.sendMutation(ctx, target, item)
		}
	}
}

type peerTarget struct {
	id      string
	address string
	http    string
}

func (e *Engine) targets() []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, member := range e.members.AlivePeers() {
		seen[member.GossipAddress] = struct{}{}
		result = append(result, member.NodeID)
	}
	for _, seed := range e.config.Seeds {
		if seed == e.config.AdvertiseAddress {
			continue
		}
		if _, ok := seen[seed]; ok {
			continue
		}
		result = append(result, "seed:"+seed)
	}
	return result
}

func (e *Engine) targetFor(key string) peerTarget {
	if len(key) > 5 && key[:5] == "seed:" {
		return peerTarget{id: key, address: key[5:]}
	}
	member, ok := e.members.Get(key)
	if !ok {
		return peerTarget{}
	}
	return peerTarget{id: member.NodeID, address: member.GossipAddress, http: member.HTTPAddress}
}

func (e *Engine) digestsForRound() []registry.ShardDigest {
	all := e.registry.Digests()
	const perRound = 2
	if len(all) <= perRound {
		return all
	}
	e.mu.Lock()
	start := e.digestCursor % len(all)
	e.digestCursor = (e.digestCursor + perRound) % len(all)
	e.mu.Unlock()
	result := make([]registry.ShardDigest, 0, perRound)
	for offset := range perRound {
		result = append(result, all[(start+offset)%len(all)])
	}
	return result
}

func (e *Engine) sendPing(ctx context.Context, target peerTarget) {
	payload := PingPayload{Self: e.members.Self(), Members: []model.Member{}, Digests: e.digestsForRound()}
	envelope, err := e.auth.NewEnvelope(MessagePing, e.config.NodeID, payload)
	if err != nil {
		return
	}
	encoded, err := e.auth.Encode(envelope)
	if err != nil {
		return
	}
	e.mu.Lock()
	e.pings[envelope.MessageID] = time.Now()
	e.mu.Unlock()
	if err := e.transport.Send(ctx, target.id, target.address, encoded); err != nil {
		e.logger.Debug("gossip ping failed", "peer", target.id, "error", err)
		return
	}
	if e.counters != nil {
		e.counters.GossipSent()
	}
}

func (e *Engine) sendMembers(ctx context.Context, target peerTarget) {
	for _, member := range e.membersForRound() {
		envelope, err := e.auth.NewEnvelope(MessageMembers, e.config.NodeID, []model.Member{member})
		if err != nil {
			continue
		}
		encoded, err := e.auth.Encode(envelope)
		if err != nil {
			continue
		}
		if err := e.transport.Send(ctx, target.id, target.address, encoded); err == nil && e.counters != nil {
			e.counters.GossipSent()
		}
	}
}

func (e *Engine) membersForRound() []model.Member {
	all := e.members.Snapshot()
	const perRound = 4
	if len(all) <= perRound {
		return all
	}
	e.mu.Lock()
	start := e.memberCursor % len(all)
	e.memberCursor = (e.memberCursor + perRound) % len(all)
	e.mu.Unlock()
	result := make([]model.Member, 0, perRound)
	for offset := range perRound {
		result = append(result, all[(start+offset)%len(all)])
	}
	return result
}

func (e *Engine) pendingForRound() []pendingMutation {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]pendingMutation, 0, len(e.pending))
	for eventID, item := range e.pending {
		result = append(result, item)
		item.rounds--
		if item.rounds <= 0 {
			delete(e.pending, eventID)
		} else {
			e.pending[eventID] = item
		}
	}
	return result
}

func (e *Engine) sendMutation(ctx context.Context, target peerTarget, item pendingMutation) {
	payload := DeltaPayload{Mutation: item.mutation, Hop: item.hop, Trace: item.trace, TraceOriginNodeID: item.originNodeID, TraceOriginAddress: item.originAddress}
	envelope, err := e.auth.NewEnvelope(MessageDelta, e.config.NodeID, payload)
	if err != nil {
		return
	}
	encoded, err := e.auth.Encode(envelope)
	if errors.Is(err, ErrMessageTooLarge) {
		e.sendSyncRequired(ctx, target)
		return
	}
	if err != nil {
		return
	}
	if err := e.transport.Send(ctx, target.id, target.address, encoded); err == nil && e.counters != nil {
		e.counters.GossipSent()
	}
}

func (e *Engine) sendSyncRequired(ctx context.Context, target peerTarget) {
	envelope, _ := e.auth.NewEnvelope(MessageSyncRequired, e.config.NodeID, map[string]string{"reason": "delta_too_large"})
	encoded, err := e.auth.Encode(envelope)
	if err == nil {
		_ = e.transport.Send(ctx, target.id, target.address, encoded)
	}
}

func (e *Engine) handleDatagram(ctx context.Context, packet []byte, remote *net.UDPAddr) {
	envelope, err := e.auth.DecodeAndVerify(packet)
	if err != nil {
		if e.counters != nil {
			e.counters.GossipRejected()
		}
		e.logger.Debug("rejected gossip packet", "remote", remote.String(), "error", err)
		return
	}
	if e.counters != nil {
		e.counters.GossipReceived()
	}
	switch envelope.Type {
	case MessagePing:
		e.handlePing(ctx, envelope, remote)
	case MessageAck:
		e.handleAck(envelope)
	case MessageDelta:
		e.handleDelta(ctx, envelope)
	case MessageMembers:
		var members []model.Member
		if json.Unmarshal(envelope.Payload, &members) == nil {
			e.mergeMembers(members)
		}
	case MessageSyncRequired:
		e.triggerSync(envelope.Sender)
	case MessageReceipt:
		e.handleReceipt(envelope)
	}
}

func (e *Engine) handlePing(ctx context.Context, envelope Envelope, remote *net.UDPAddr) {
	var payload PingPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return
	}
	e.members.ObserveAlive(payload.Self, 0, time.Now())
	e.mergeMembers(payload.Members)
	if e.needsSync(payload.Digests) {
		e.triggerSync(payload.Self.NodeID)
	}
	ack := AckPayload{ReplyTo: envelope.MessageID, Self: e.members.Self(), Members: []model.Member{}, Digests: e.digestsForRound()}
	reply, _ := e.auth.NewEnvelope(MessageAck, e.config.NodeID, ack)
	encoded, err := e.auth.Encode(reply)
	if err == nil {
		_ = e.transport.Send(ctx, payload.Self.NodeID, remote.String(), encoded)
	}
	e.sendMembers(ctx, peerTarget{id: payload.Self.NodeID, address: remote.String()})
}

func (e *Engine) handleAck(envelope Envelope) {
	var payload AckPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return
	}
	e.mu.Lock()
	sent, ok := e.pings[payload.ReplyTo]
	delete(e.pings, payload.ReplyTo)
	e.mu.Unlock()
	latency := time.Duration(0)
	if ok {
		latency = time.Since(sent)
	}
	e.members.ObserveAlive(payload.Self, latency, time.Now())
	e.mergeMembers(payload.Members)
	if e.needsSync(payload.Digests) {
		e.triggerSync(payload.Self.NodeID)
	}
}

func (e *Engine) handleDelta(ctx context.Context, envelope Envelope) {
	started := time.Now()
	var payload DeltaPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return
	}
	e.mu.Lock()
	e.incomingMeta[payload.Mutation.EventID] = traceMeta{hop: payload.Hop + 1, originNodeID: payload.TraceOriginNodeID, originAddress: payload.TraceOriginAddress, trace: payload.Trace}
	e.mu.Unlock()
	result, err := e.applier.ApplyRemote(payload.Mutation)
	resultName := "REJECTED"
	if err == nil {
		switch {
		case result.Applied:
			resultName = "APPLIED"
		case result.Duplicate:
			resultName = "DUPLICATE"
		case result.Stale:
			resultName = "REJECTED"
		}
	}
	if !result.Applied {
		e.mu.Lock()
		delete(e.incomingMeta, payload.Mutation.EventID)
		e.mu.Unlock()
	}
	if payload.Trace && payload.TraceOriginAddress != "" {
		receipt := ReceiptPayload{EventID: payload.Mutation.EventID, AttemptID: envelope.MessageID, SourceNodeID: envelope.Sender, TargetNodeID: e.config.NodeID, Hop: payload.Hop, Result: resultName, LatencyMS: float64(time.Since(started).Microseconds()) / 1000}
		reply, _ := e.auth.NewEnvelope(MessageReceipt, e.config.NodeID, receipt)
		encoded, encodeErr := e.auth.Encode(reply)
		if encodeErr == nil {
			_ = e.transport.Send(ctx, payload.TraceOriginNodeID, payload.TraceOriginAddress, encoded)
		}
	}
}

func (e *Engine) handleReceipt(envelope Envelope) {
	var receipt ReceiptPayload
	if json.Unmarshal(envelope.Payload, &receipt) != nil {
		return
	}
	e.events.Publish(events.Event{Type: events.GossipHop, EventID: receipt.EventID, EntityKey: receipt.EventID, Payload: events.Payload(map[string]any{"event_id": receipt.EventID}), Delivery: &events.Delivery{AttemptID: receipt.AttemptID, SourceNode: receipt.SourceNodeID, TargetNode: receipt.TargetNodeID, Hop: receipt.Hop, Result: receipt.Result, LatencyMS: receipt.LatencyMS}})
}

func (e *Engine) mergeMembers(members []model.Member) {
	for _, member := range members {
		e.members.Merge(member)
	}
}

func (e *Engine) hasDigestMismatch(remote []registry.ShardDigest) bool {
	for _, digest := range remote {
		local, ok := e.registry.Digest(digest.Shard)
		if !ok || local.SHA256 != digest.SHA256 {
			return true
		}
	}
	return false
}

func (e *Engine) needsSync(remote []registry.ShardDigest) bool {
	return !e.ready.Load() || e.hasDigestMismatch(remote)
}

func (e *Engine) recordSyncResult(err error) bool {
	if err != nil {
		return false
	}
	e.ready.Store(true)
	return true
}

func (e *Engine) triggerSync(nodeID string) {
	member, ok := e.members.Get(nodeID)
	if !ok || member.HTTPAddress == "" {
		return
	}
	e.mu.Lock()
	if e.syncing[nodeID] {
		e.mu.Unlock()
		return
	}
	e.syncing[nodeID] = true
	e.mu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			e.mu.Lock()
			delete(e.syncing, nodeID)
			e.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		syncErr := e.SyncPeer(ctx, member)
		if !e.recordSyncResult(syncErr) {
			e.logger.Debug("anti-entropy sync failed", "peer", nodeID, "error", syncErr)
		}
	}()
}

func (e *Engine) antiEntropyRound(ctx context.Context) {
	peers := e.members.AlivePeers()
	if len(peers) == 0 {
		return
	}
	selected := e.selector.Pick(memberIDs(peers), 1)
	if len(selected) == 1 {
		e.triggerSync(selected[0])
	}
}

func memberIDs(members []model.Member) []string {
	result := make([]string, 0, len(members))
	for _, member := range members {
		result = append(result, member.NodeID)
	}
	return result
}

func (e *Engine) Close() error {
	return e.transport.Close()
}
