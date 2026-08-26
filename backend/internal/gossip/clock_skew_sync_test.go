package gossip_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/events"
	"minieureka/internal/gossip"
	"minieureka/internal/model"
	"minieureka/internal/registry"
	"minieureka/internal/service"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSyncPeerRejectsMutationBeyondClockSkew(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	localClock, err := clock.New(
		"node-local",
		clock.WithSource(clock.SourceFunc(func() time.Time { return now })),
		clock.WithMaxFutureSkew(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	store, err := registry.New(8, registry.WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	localService := service.New(store, localClock, events.New(64, "node-local", "boot-local"), nil, service.Options{
		NodeID: "node-local",
		Now:    func() time.Time { return now },
	})

	remoteVersion := model.Version{
		PhysicalMillis: now.Add(time.Hour).UnixMilli(),
		OriginNodeID:   "node-fast",
	}
	mutation := model.Mutation{
		Kind:    model.MutationRegister,
		EventID: "event-from-fast-node",
		Record: model.Instance{
			Service: "orders", InstanceID: "orders-fast-1", RegistrationID: "registration-fast-1",
			Host: "127.0.0.1", Port: 8080, Protocol: model.ProtocolHTTP, Metadata: map[string]string{},
			Status: model.StatusActive, StatusReason: model.ReasonRegistered, Generation: 1,
			LeaseID: "lease-fast-1", LeaseEpoch: remoteVersion, Version: remoteVersion, OriginNodeID: "node-fast",
			RegisteredAt: now, LastHeartbeatAt: now, UpdatedAt: now,
		},
		RemainingTTLMillis: 30_000,
	}
	responseBody, err := json.Marshal(gossip.AntiEntropyResponse{
		Mutations: []model.Mutation{mutation},
	})
	if err != nil {
		t.Fatal(err)
	}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	engine := gossip.NewEngine(
		gossip.EngineConfig{NodeID: "node-local"},
		gossip.NewAuthenticator("cluster-secret", "cluster-a", time.Minute, 1200),
		nil, nil, nil, store, localService, nil, nil, nil,
	)
	if err := engine.SyncPeer(context.Background(), model.Member{HTTPAddress: "http://node-fast.test"}); err != nil {
		t.Fatalf("SyncPeer() error = %v", err)
	}
	if instances := localService.Discover("orders"); len(instances) != 0 {
		t.Fatalf("Discover() returned %d instance(s) from fast clock, want 0", len(instances))
	}
}
