package gossip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"minieureka/internal/model"
	"minieureka/internal/registry"
)

var syncTestScheme atomic.Uint64

func TestSyncPeerReadsResponseBeforeClosingBody(t *testing.T) {
	responseJSON := []byte(`{"mutations":[],"fences":[],"digests":[],"next_cursor":null}`)
	responseBody := &closeAwareBody{reader: bytes.NewReader(responseJSON)}
	scheme := fmt.Sprintf("minieureka-sync-%d", syncTestScheme.Add(1))
	http.DefaultTransport.(*http.Transport).RegisterProtocol(scheme, syncRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          responseBody,
			ContentLength: int64(len(responseJSON)),
			Request:       request,
		}, nil
	}))

	store, err := registry.New(8)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(
		EngineConfig{NodeID: "node-1"},
		NewAuthenticator("secret", "cluster", time.Minute, 1200),
		nil, nil, nil, store, nil, nil, nil, nil,
	)
	peer := model.Member{NodeID: "node-2", HTTPAddress: scheme + "://node-2"}

	if err := engine.SyncPeer(context.Background(), peer); err != nil {
		t.Fatalf("SyncPeer() error = %v", err)
	}
	if !responseBody.closed.Load() {
		t.Fatal("SyncPeer() did not close the response body")
	}
}

type syncRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip syncRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type closeAwareBody struct {
	reader *bytes.Reader
	closed atomic.Bool
}

func (body *closeAwareBody) Read(buffer []byte) (int, error) {
	if body.closed.Load() {
		return 0, errors.New("response body is closed")
	}
	return body.reader.Read(buffer)
}

func (body *closeAwareBody) Close() error {
	body.closed.Store(true)
	return nil
}
