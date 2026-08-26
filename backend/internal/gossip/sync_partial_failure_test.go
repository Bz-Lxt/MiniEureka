package gossip_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/events"
	"minieureka/internal/gossip"
	"minieureka/internal/model"
	"minieureka/internal/registry"
	"minieureka/internal/service"
)

func TestSyncPeerReturnsErrorWhenLaterPageFails(t *testing.T) {
	now := time.Now().UTC()
	version := model.Version{PhysicalMillis: now.UnixMilli(), Logical: 1, OriginNodeID: "node-2"}
	mutation := model.Mutation{
		Kind: model.MutationRegister,
		Record: model.Instance{
			Service: "orders", InstanceID: "orders-1", RegistrationID: "registration-1",
			Host: "127.0.0.1", Port: 8080, Protocol: model.ProtocolHTTP, Metadata: map[string]string{},
			Status: model.StatusActive, StatusReason: model.ReasonRegistered, Generation: 1,
			LeaseID: "lease-1", LeaseEpoch: version, Version: version, OriginNodeID: "node-2",
			RegisteredAt: now, LastHeartbeatAt: now, UpdatedAt: now,
		},
		EventID: "event-1", RemainingTTLMillis: 30_000,
	}
	nextCursor := 1
	transport := &pagedFailureTransport{
		firstPage: gossip.AntiEntropyResponse{
			Mutations: []model.Mutation{mutation}, Fences: []registry.Fence{},
			Digests: []registry.ShardDigest{}, NextCursor: &nextCursor,
		},
	}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = originalTransport }()

	store, err := registry.New(8)
	if err != nil {
		t.Fatal(err)
	}
	hlc, err := clock.New("node-1")
	if err != nil {
		t.Fatal(err)
	}
	ring := events.New(16, "node-1", "boot-1")
	svc := service.New(store, hlc, ring, nil, service.Options{NodeID: "node-1", LeaseTTL: 30 * time.Second})
	engine := gossip.NewEngine(
		gossip.EngineConfig{NodeID: "node-1", Seeds: []string{"node-2:7946"}},
		gossip.NewAuthenticator("secret", "cluster", time.Minute, 1200), nil, gossip.NewSelector(1),
		nil, store, svc, ring, nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	err = engine.SyncPeer(context.Background(), model.Member{HTTPAddress: "http://node-2:8080"})
	if err == nil {
		t.Fatal("SyncPeer() returned nil after the peer rejected a later page")
	}
	if got := transport.Cursors(); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("anti-entropy cursors = %v, want [0 1]", got)
	}
	if records := svc.Discover("orders"); len(records) != 1 || records[0].InstanceID != "orders-1" {
		t.Fatalf("records applied before failure = %#v", records)
	}
}

type pagedFailureTransport struct {
	mu        sync.Mutex
	firstPage gossip.AntiEntropyResponse
	cursors   []int
}

func (t *pagedFailureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var payload gossip.AntiEntropyRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.cursors = append(t.cursors, payload.Cursor)
	t.mu.Unlock()
	if payload.Cursor == 0 {
		body, err := json.Marshal(t.firstPage)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    request,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString("temporarily unavailable")),
		Request:    request,
	}, nil
}

func (t *pagedFailureTransport) Cursors() []int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]int(nil), t.cursors...)
}
