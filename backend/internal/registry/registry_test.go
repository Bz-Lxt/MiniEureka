package registry

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"minieureka/internal/model"
)

var baseTime = time.Unix(1_700_000_000, 0).UTC()

func version(physical int64, logical uint64) model.Version {
	return model.Version{PhysicalMillis: physical, Logical: logical, OriginNodeID: "node-1"}
}

func record(service, instance, registration, lease string, generation uint64, epoch, revision model.Version) model.Instance {
	return model.Instance{
		Service: service, InstanceID: instance, RegistrationID: registration,
		Host: "127.0.0.1", Port: 8080, Protocol: model.ProtocolHTTP,
		Metadata: map[string]string{"zone": "east"}, Status: model.StatusActive,
		StatusReason: model.ReasonRegistered, Generation: generation, LeaseID: lease,
		LeaseEpoch: epoch, Version: revision, OriginNodeID: revision.OriginNodeID,
		RegisteredAt: baseTime, LastHeartbeatAt: baseTime, UpdatedAt: baseTime,
	}
}

func mutation(kind model.MutationKind, value model.Instance, eventID string) model.Mutation {
	switch kind {
	case model.MutationRegister:
		value.Status, value.StatusReason, value.EvictedAt = model.StatusActive, model.ReasonRegistered, nil
	case model.MutationHeartbeat:
		value.Status, value.StatusReason, value.EvictedAt = model.StatusActive, model.ReasonHeartbeatOK, nil
	case model.MutationDelayed:
		value.Status, value.StatusReason, value.EvictedAt = model.StatusDelayed, model.ReasonHeartbeatDelayed, nil
	case model.MutationTTLExpire:
		at := value.UpdatedAt
		value.Status, value.StatusReason, value.EvictedAt = model.StatusEvicted, model.ReasonTTLExpired, &at
	case model.MutationDeregister:
		at := value.UpdatedAt
		value.Status, value.StatusReason, value.EvictedAt = model.StatusEvicted, model.ReasonDeregistered, &at
	case model.MutationDemoOffline:
		at := value.UpdatedAt
		value.Status, value.StatusReason, value.EvictedAt = model.StatusEvicted, model.ReasonDemoOffline, &at
	}
	return model.Mutation{Kind: kind, Record: value, EventID: eventID, RemainingTTLMillis: 30_000}
}

func mustRegistry(t *testing.T, options ...Option) *Registry {
	t.Helper()
	r, err := New(DefaultShards, options...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func apply(t *testing.T, r *Registry, mutation model.Mutation) ApplyResult {
	t.Helper()
	result, err := r.Apply(mutation)
	if err != nil {
		t.Fatalf("Apply(%s): %v", mutation.EventID, err)
	}
	return result
}

func TestNewRejectsNonPowerOfTwo(t *testing.T) {
	for _, count := range []int{-1, 0, 3, 63} {
		if _, err := New(count); !errors.Is(err, ErrInvalidShardCount) {
			t.Fatalf("New(%d) error = %v", count, err)
		}
	}
	r, err := New(64)
	if err != nil || r.ShardCount() != 64 {
		t.Fatalf("New(64) = (%v, %v)", r, err)
	}
}

func TestRegistryCRUDQueriesAndDefensiveCopies(t *testing.T) {
	r := mustRegistry(t)
	for _, input := range []model.Instance{
		record("payments", "p-2", "reg-p2", "lease-p2", 1, version(1, 0), version(1, 0)),
		record("orders", "o-2", "reg-o2", "lease-o2", 1, version(2, 0), version(2, 0)),
		record("orders", "o-1", "reg-o1", "lease-o1", 1, version(3, 0), version(3, 0)),
	} {
		if result := apply(t, r, mutation(model.MutationRegister, input, "evt-"+input.InstanceID)); !result.Applied {
			t.Fatalf("register %s was not applied", input.Key())
		}
	}
	got, ok := r.Get("orders", "o-1")
	if !ok {
		t.Fatal("Get did not find record")
	}
	got.Metadata["zone"] = "corrupt"
	again, _ := r.Get("orders", "o-1")
	if again.Metadata["zone"] != "east" {
		t.Fatal("Get exposed internal metadata")
	}
	list := r.List("orders")
	if len(list) != 2 || list[0].InstanceID != "o-1" || list[1].InstanceID != "o-2" {
		t.Fatalf("List order = %+v", list)
	}
	counts := r.Counts()
	if counts.Services != 2 || counts.Instances != 3 || counts.Active != 3 {
		t.Fatalf("Counts = %+v", counts)
	}
}

func TestApplyRegistrationAndOperationIdempotency(t *testing.T) {
	r := mustRegistry(t)
	first := record("orders", "one", "registration-one", "lease-one", 1, version(10, 0), version(10, 0))
	registered := apply(t, r, mutation(model.MutationRegister, first, "event-register"))
	if !registered.Applied {
		t.Fatal("initial register was not applied")
	}
	retry := record("orders", "one", "registration-one", "different-lease", 2, version(20, 0), version(20, 0))
	concurrent := apply(t, r, mutation(model.MutationRegister, retry, "event-retry"))
	if !concurrent.Applied || concurrent.Record.LeaseID != "different-lease" {
		t.Fatalf("concurrent same-intent registration did not converge by epoch = %+v", concurrent)
	}

	heartbeat := retry.Clone()
	heartbeat.Version = version(21, 0)
	heartbeat.UpdatedAt = baseTime.Add(time.Second)
	heartbeat.LastHeartbeatAt = heartbeat.UpdatedAt
	heartbeatMutation := mutation(model.MutationHeartbeat, heartbeat, "event-heartbeat")
	heartbeatMutation.OperationID = "operation-one"
	if result := apply(t, r, heartbeatMutation); !result.Applied {
		t.Fatalf("heartbeat = %+v", result)
	}
	retryHeartbeat := heartbeatMutation
	retryHeartbeat.Record.Version = version(22, 0)
	retryHeartbeat.EventID = "event-heartbeat-retry"
	result := apply(t, r, retryHeartbeat)
	if !result.Duplicate || result.Record.Version != version(21, 0) || result.EventID != "event-heartbeat" {
		t.Fatalf("operation retry advanced state: %+v", result)
	}
}

func TestDelayedProjectionExportsUnderlyingLiveMutation(t *testing.T) {
	now := baseTime
	r := mustRegistry(t, WithNow(func() time.Time { return now }))
	value := record("orders", "projection", "reg-projection", "lease-projection", 1, version(100, 0), version(100, 0))
	registered := mutation(model.MutationRegister, value, "event-projection")
	registered.RemainingTTLMillis = 30_000
	if result := apply(t, r, registered); !result.Applied {
		t.Fatal("register was not applied")
	}
	stored, _ := r.Get("orders", "projection")
	if _, ok := r.MarkDelayed(stored.Key(), stored.LeaseID, stored.Version, now.Add(15*time.Second)); !ok {
		t.Fatal("MarkDelayed did not apply")
	}
	mutations, ok := r.MutationsForShard(r.ShardIndex("orders"))
	if !ok || len(mutations) != 1 {
		t.Fatalf("MutationsForShard = %#v, %v", mutations, ok)
	}
	if mutations[0].Record.Status != model.StatusActive || mutations[0].Kind != model.MutationRegister {
		t.Fatalf("exported mutation = %#v", mutations[0])
	}
	if err := mutations[0].Validate(); err != nil {
		t.Fatalf("exported mutation is invalid: %v", err)
	}
}

func TestApplyTerminalAndTTLMergeRules(t *testing.T) {
	r := mustRegistry(t)
	base := record("orders", "one", "reg-one", "lease-one", 1, version(10, 0), version(10, 0))
	apply(t, r, mutation(model.MutationRegister, base, "register"))

	ttl := base.Clone()
	ttl.Version, ttl.UpdatedAt = version(12, 0), baseTime.Add(2*time.Second)
	if result := apply(t, r, mutation(model.MutationTTLExpire, ttl, "ttl-expire")); !result.Applied {
		t.Fatalf("TTL expiry = %+v", result)
	}
	heartbeat := base.Clone()
	heartbeat.Version, heartbeat.UpdatedAt = version(13, 0), baseTime.Add(3*time.Second)
	heartbeat.LastHeartbeatAt = heartbeat.UpdatedAt
	if result := apply(t, r, mutation(model.MutationHeartbeat, heartbeat, "late-heartbeat")); !result.Applied || result.Record.Status != model.StatusActive {
		t.Fatalf("higher heartbeat did not beat TTL: %+v", result)
	}

	deregister := heartbeat.Clone()
	deregister.Version, deregister.UpdatedAt = version(14, 0), baseTime.Add(4*time.Second)
	if result := apply(t, r, mutation(model.MutationDeregister, deregister, "deregister")); !result.Applied {
		t.Fatalf("deregister = %+v", result)
	}
	heartbeat.Version, heartbeat.UpdatedAt = version(99, 0), baseTime.Add(5*time.Second)
	if result := apply(t, r, mutation(model.MutationHeartbeat, heartbeat, "forbidden-heartbeat")); !result.Stale {
		t.Fatalf("explicit tombstone was resurrected: %+v", result)
	}

	newLease := record("orders", "one", "reg-two", "lease-two", 2, version(100, 0), version(100, 0))
	if result := apply(t, r, mutation(model.MutationRegister, newLease, "reregister")); !result.Applied || result.Record.LeaseID != "lease-two" {
		t.Fatalf("new lease could not resurrect: %+v", result)
	}
}

func TestExplicitEvictionConvergesRegardlessOfArrivalOrder(t *testing.T) {
	live := record("orders", "reordered", "reg-reordered", "lease-reordered", 1, version(100, 0), version(200, 0))
	live.StatusReason = model.ReasonHeartbeatOK
	heartbeat := mutation(model.MutationHeartbeat, live, "event-heartbeat-newer")
	explicitRecord := live.Clone()
	explicitRecord.Version = version(150, 0)
	explicitRecord.UpdatedAt = baseTime.Add(time.Second)
	explicit := mutation(model.MutationDeregister, explicitRecord, "event-deregister-older-version")

	firstTerminal := mustRegistry(t)
	apply(t, firstTerminal, explicit)
	apply(t, firstTerminal, heartbeat)
	first, _ := firstTerminal.Get("orders", "reordered")

	firstHeartbeat := mustRegistry(t)
	apply(t, firstHeartbeat, heartbeat)
	apply(t, firstHeartbeat, explicit)
	second, _ := firstHeartbeat.Get("orders", "reordered")

	if first.StatusReason != model.ReasonDeregistered || second.StatusReason != model.ReasonDeregistered || !first.Equal(second) {
		t.Fatalf("arrival order diverged: first=%#v second=%#v", first, second)
	}
}

func TestEqualVersionUsesMutationPriorityThenEventID(t *testing.T) {
	r := mustRegistry(t)
	base := record("orders", "one", "reg", "lease", 1, version(10, 0), version(10, 0))
	apply(t, r, mutation(model.MutationRegister, base, "event-a"))
	heartbeat := mutation(model.MutationHeartbeat, base, "event-b")
	if result := apply(t, r, heartbeat); !result.Applied {
		t.Fatalf("heartbeat priority should beat register: %+v", result)
	}
	ttl := mutation(model.MutationTTLExpire, base, "event-a")
	if result := apply(t, r, ttl); !result.Applied {
		t.Fatalf("TTL priority should beat heartbeat: %+v", result)
	}
	deregister := mutation(model.MutationDeregister, base, "event-a")
	if result := apply(t, r, deregister); !result.Applied {
		t.Fatalf("explicit priority should beat TTL: %+v", result)
	}
}

func TestGCLeavesFenceThatBlocksOldPartitionState(t *testing.T) {
	r := mustRegistry(t)
	base := record("orders", "one", "reg", "lease", 1, version(10, 0), version(10, 0))
	apply(t, r, mutation(model.MutationRegister, base, "register"))
	tombstone := base.Clone()
	tombstone.Version, tombstone.UpdatedAt = version(20, 0), baseTime.Add(time.Minute)
	apply(t, r, mutation(model.MutationDeregister, tombstone, "delete"))
	removed := r.GC(baseTime.Add(10*time.Minute), 5*time.Minute)
	if len(removed) != 1 {
		t.Fatalf("GC removed %d records", len(removed))
	}
	if _, ok := r.Get("orders", "one"); ok {
		t.Fatal("full tombstone retained after GC")
	}
	if _, ok := r.GetFence(base.Key()); !ok {
		t.Fatal("version fence was collected")
	}
	oldHeartbeat := base.Clone()
	oldHeartbeat.Version = version(99, 0)
	if result := apply(t, r, mutation(model.MutationHeartbeat, oldHeartbeat, "old-partition")); !result.Stale {
		t.Fatalf("fence allowed old epoch: %+v", result)
	}
	newLease := record("orders", "one", "reg-new", "lease-new", 2, version(100, 0), version(100, 0))
	if result := apply(t, r, mutation(model.MutationRegister, newLease, "new-register")); !result.Applied {
		t.Fatalf("fence blocked new epoch: %+v", result)
	}
}

func TestLocalTTLProjectionAndDuplicateDoesNotExtendDeadline(t *testing.T) {
	now := baseTime
	r := mustRegistry(t, WithNow(func() time.Time { return now }))
	base := record("orders", "one", "reg", "lease", 1, version(10, 0), version(10, 0))
	register := mutation(model.MutationRegister, base, "register")
	register.RemainingTTLMillis = 30_000
	first := apply(t, r, register)
	deadline := first.Record.LeaseDeadline
	now = now.Add(20 * time.Second)
	duplicate := apply(t, r, register)
	if !duplicate.Duplicate || !duplicate.Record.LeaseDeadline.Equal(deadline) {
		t.Fatalf("duplicate extended deadline: before=%v after=%v", deadline, duplicate.Record.LeaseDeadline)
	}
	if _, changed := r.MarkDelayed(base.Key(), "wrong-lease", base.Version, now); changed {
		t.Fatal("stale delay task changed record")
	}
	if projected, changed := r.MarkDelayed(base.Key(), base.LeaseID, base.Version, now); !changed || projected.Status != model.StatusDelayed {
		t.Fatalf("MarkDelayed = (%+v, %v)", projected, changed)
	}
	if expired := r.Expired(deadline.Add(-time.Millisecond)); len(expired) != 0 {
		t.Fatalf("expired early: %+v", expired)
	}
	if expired := r.Expired(deadline); len(expired) != 1 {
		t.Fatalf("expired candidates = %d", len(expired))
	}
}

func TestDigestIsInsertionOrderIndependentAndIgnoresLocalDelayProjection(t *testing.T) {
	one := mustRegistry(t)
	two := mustRegistry(t)
	records := []model.Instance{
		record("orders", "a", "reg-a", "lease-a", 1, version(10, 0), version(10, 0)),
		record("orders", "b", "reg-b", "lease-b", 1, version(11, 0), version(11, 0)),
	}
	for _, value := range records {
		apply(t, one, mutation(model.MutationRegister, value, "event-"+value.InstanceID))
	}
	for index := len(records) - 1; index >= 0; index-- {
		value := records[index]
		apply(t, two, mutation(model.MutationRegister, value, "event-"+value.InstanceID))
	}
	shard := one.ShardIndex("orders")
	digestOne, _ := one.Digest(shard)
	digestTwo, _ := two.Digest(shard)
	if digestOne.SHA256 != digestTwo.SHA256 {
		t.Fatalf("digests differ by insertion order: %s != %s", digestOne.SHA256, digestTwo.SHA256)
	}
	one.MarkDelayed(records[0].Key(), records[0].LeaseID, records[0].Version, baseTime.Add(time.Second))
	delayedDigest, _ := one.Digest(shard)
	if delayedDigest.SHA256 != digestOne.SHA256 {
		t.Fatalf("local projection changed authoritative digest: %s != %s", delayedDigest.SHA256, digestOne.SHA256)
	}
}

func TestExportPreservesMutationIdentityAndDecreasesTTL(t *testing.T) {
	now := baseTime
	r := mustRegistry(t, WithNow(func() time.Time { return now }))
	value := record("orders", "one", "reg", "lease", 1, version(10, 0), version(10, 0))
	registered := mutation(model.MutationRegister, value, "original-event")
	registered.RemainingTTLMillis = 30_000
	apply(t, r, registered)
	now = now.Add(7 * time.Second)
	exported := r.Export()
	if len(exported) != 1 {
		t.Fatalf("Export length = %d", len(exported))
	}
	if exported[0].Kind != model.MutationRegister || exported[0].EventID != "original-event" {
		t.Fatalf("mutation identity lost: %+v", exported[0])
	}
	if exported[0].RemainingTTLMillis != 23_000 {
		t.Fatalf("remaining TTL = %d, want 23000", exported[0].RemainingTTLMillis)
	}
	now = now.Add(time.Minute)
	exported = r.Export()
	if exported[0].RemainingTTLMillis != 0 {
		t.Fatalf("expired remaining TTL = %d", exported[0].RemainingTTLMillis)
	}
}

func TestApplyFenceConvergesWithoutFullTombstone(t *testing.T) {
	r := mustRegistry(t)
	value := record("orders", "one", "reg", "lease", 1, version(10, 0), version(10, 0))
	apply(t, r, mutation(model.MutationRegister, value, "register"))
	fence := Fence{
		Key: value.Key(), LeaseEpoch: value.LeaseEpoch, Version: version(20, 0),
		Kind: model.MutationDeregister, EventID: "remote-delete", Generation: 1,
	}
	if !r.ApplyFence(fence) {
		t.Fatal("newer fence was not applied")
	}
	if _, exists := r.Get(value.Service, value.InstanceID); exists {
		t.Fatal("winning fence did not remove dominated full record")
	}
	if r.ApplyFence(fence) {
		t.Fatal("duplicate fence reported applied")
	}
	newLease := record("orders", "one", "reg-new", "lease-new", 2, version(30, 0), version(30, 0))
	if result := apply(t, r, mutation(model.MutationRegister, newLease, "register-new")); !result.Applied {
		t.Fatalf("new epoch did not beat fence: %+v", result)
	}
	olderFence := fence
	olderFence.Version, olderFence.EventID = version(21, 0), "old-fence-retry"
	if r.ApplyFence(olderFence) {
		t.Fatal("old-epoch fence removed newer lease")
	}
}

func TestRegistryConcurrentReadWrite(t *testing.T) {
	r := mustRegistry(t)
	const workers = 12
	const instances = 100
	var wg sync.WaitGroup
	for worker := range workers {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range instances {
				id := fmt.Sprintf("instance-%03d", index)
				epoch := version(int64(1_000+index), uint64(worker))
				value := record("service", id, fmt.Sprintf("reg-%d-%d", worker, index), fmt.Sprintf("lease-%d-%d", worker, index), uint64(worker+1), epoch, epoch)
				_, _ = r.Apply(mutation(model.MutationRegister, value, fmt.Sprintf("event-%d-%d", worker, index)))
				_, _ = r.Get("service", id)
				_ = r.List("service")
			}
		}()
	}
	wg.Wait()
	if got := r.Counts().Instances; got != instances {
		t.Fatalf("instance count = %d, want %d", got, instances)
	}
}

func BenchmarkRegistryGet(b *testing.B) {
	r, _ := New(DefaultShards)
	value := record("orders", "one", "reg", "lease", 1, version(10, 0), version(10, 0))
	_, _ = r.Apply(mutation(model.MutationRegister, value, "register"))
	b.ReportAllocs()
	for b.Loop() {
		_, _ = r.Get("orders", "one")
	}
}

func BenchmarkRegistryApplyHeartbeat(b *testing.B) {
	r, _ := New(DefaultShards)
	value := record("orders", "one", "reg", "lease", 1, version(10, 0), version(10, 0))
	_, _ = r.Apply(mutation(model.MutationRegister, value, "register"))
	b.ReportAllocs()
	var logical uint64
	for b.Loop() {
		logical++
		value.Version = version(11, logical)
		_, _ = r.Apply(mutation(model.MutationHeartbeat, value, fmt.Sprintf("heartbeat-%d", logical)))
	}
}
