package gossip

import (
	"testing"
	"time"

	"minieureka/internal/model"
	"minieureka/internal/registry"
)

func TestAntiEntropyURL(t *testing.T) {
	t.Parallel()
	got, err := antiEntropyURL("http://registry-2:8080/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://registry-2:8080/internal/v1/anti-entropy" {
		t.Fatalf("url = %q", got)
	}
}

func TestBuildAntiEntropyResponseReturnsOnlyMismatchedShards(t *testing.T) {
	t.Parallel()
	store, err := registry.New(8)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	version := model.Version{PhysicalMillis: now.UnixMilli(), OriginNodeID: "node-1"}
	record := model.Instance{
		Service: "orders", InstanceID: "orders-1", RegistrationID: "registration-1",
		Host: "127.0.0.1", Port: 8080, Protocol: model.ProtocolHTTP, Metadata: map[string]string{},
		Status: model.StatusActive, StatusReason: model.ReasonRegistered, Generation: 1,
		LeaseID: "lease-1", LeaseEpoch: version, Version: version, OriginNodeID: "node-1",
		RegisteredAt: now, LastHeartbeatAt: now, UpdatedAt: now,
	}
	mutation := model.Mutation{Kind: model.MutationRegister, Record: record, EventID: "event-1", RemainingTTLMillis: 30_000}
	if result, err := store.Apply(mutation); err != nil || !result.Applied {
		t.Fatalf("Apply() = %#v, %v", result, err)
	}
	engine := &Engine{registry: store}
	response := engine.buildAntiEntropyResponse(AntiEntropyRequest{Digests: []registry.ShardDigest{}, Cursor: 0})
	if len(response.Mutations) != 1 || response.Mutations[0].EventID != "event-1" {
		t.Fatalf("response mutations = %#v", response.Mutations)
	}
	response = engine.buildAntiEntropyResponse(AntiEntropyRequest{Digests: store.Digests(), Cursor: 0})
	if len(response.Mutations) != 0 || len(response.Fences) != 0 {
		t.Fatalf("matching response = %#v", response)
	}
}
