package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"minieureka/internal/api"
	"minieureka/internal/clock"
	"minieureka/internal/cluster"
	"minieureka/internal/events"
	"minieureka/internal/model"
)

func TestTopologyRemainsAvailableWhileMemberHookBlocks(t *testing.T) {
	hlc, err := clock.New("node-1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	hookEntered := make(chan struct{})
	releaseHook := make(chan struct{})
	var releaseOnce sync.Once
	members := cluster.NewTable(model.Member{
		NodeID: "node-1", BootID: "boot-1", HTTPAddress: "http://node-1:8080", GossipAddress: "node-1:7946",
		Status: model.MemberAlive, Incarnation: 1, LastSeenAt: now, Version: hlc.Now(),
	}, hlc, time.Second, 2*time.Second, func(model.Member) {
		close(hookEntered)
		<-releaseHook
	})
	server := api.New(api.Options{
		NodeID: "node-1", BootID: "boot-1", Members: members, Events: events.New(16, "node-1", "boot-1"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	mergeDone := make(chan struct{})
	go func() {
		defer close(mergeDone)
		members.Merge(model.Member{
			NodeID: "node-2", BootID: "boot-2", HTTPAddress: "http://node-2:8080", GossipAddress: "node-2:7946",
			Status: model.MemberAlive, Incarnation: 1, LastSeenAt: now,
			Version: model.Version{PhysicalMillis: now.UnixMilli(), OriginNodeID: "node-2"},
		})
	}()
	select {
	case <-hookEntered:
	case <-time.After(time.Second):
		t.Fatal("member hook was not called")
	}

	responseDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/topology", nil)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		responseDone <- response
	}()

	select {
	case response := <-responseDone:
		releaseOnce.Do(func() { close(releaseHook) })
		<-mergeDone
		if response.Code != http.StatusOK {
			t.Fatalf("topology status = %d, body = %s", response.Code, response.Body.String())
		}
	case <-time.After(250 * time.Millisecond):
		releaseOnce.Do(func() { close(releaseHook) })
		<-mergeDone
		<-responseDone
		t.Fatal("topology request remained blocked behind the member hook")
	}
}
