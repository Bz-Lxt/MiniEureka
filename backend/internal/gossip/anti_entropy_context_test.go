package gossip_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"minieureka/internal/gossip"
	"minieureka/internal/model"
	"minieureka/internal/registry"
)

func TestSyncPeerStopsWhenContextCanceledDuringPage(t *testing.T) {
	var requestCount atomic.Int32
	firstRequest := make(chan struct{})
	releaseFirstResponse := make(chan struct{})
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestNumber := requestCount.Add(1)
		if requestNumber == 1 {
			close(firstRequest)
			select {
			case <-request.Context().Done():
				return nil, request.Context().Err()
			case <-releaseFirstResponse:
			}
			return syncResponse(1), nil
		}
		return syncResponse(0), nil
	})
	defer func() { http.DefaultTransport = originalTransport }()

	engine := gossip.NewEngine(
		gossip.EngineConfig{NodeID: "node-local"},
		gossip.NewAuthenticator("test-secret", "test-cluster", time.Minute, 1200),
		nil, nil, nil, emptyRegistryView{}, nil, nil, nil, nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- engine.SyncPeer(ctx, model.Member{HTTPAddress: "http://peer.test"})
	}()

	select {
	case <-firstRequest:
	case <-time.After(2 * time.Second):
		t.Fatal("peer did not receive the first anti-entropy page request")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SyncPeer() error = %v, want context.Canceled", err)
		}
	case <-time.After(200 * time.Millisecond):
		close(releaseFirstResponse)
		err := <-result
		t.Fatalf("SyncPeer() did not stop after cancellation: error = %v, peer requests = %d", err, requestCount.Load())
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("peer received %d page requests after cancellation, want 1", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func syncResponse(nextCursor int) *http.Response {
	payload := gossip.AntiEntropyResponse{
		Mutations: []model.Mutation{}, Fences: []registry.Fence{}, Digests: []registry.ShardDigest{},
	}
	if nextCursor > 0 {
		payload.NextCursor = &nextCursor
	}
	body, _ := json.Marshal(payload)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

type emptyRegistryView struct{}

func (emptyRegistryView) Digests() []registry.ShardDigest { return []registry.ShardDigest{} }
func (emptyRegistryView) Digest(int) (registry.ShardDigest, bool) {
	return registry.ShardDigest{}, false
}
func (emptyRegistryView) MutationsForShard(int) ([]model.Mutation, bool) { return nil, false }
func (emptyRegistryView) Fences() []registry.Fence                       { return nil }
func (emptyRegistryView) ShardIndex(string) int                          { return 0 }
func (emptyRegistryView) ApplyFence(registry.Fence) bool                 { return false }
