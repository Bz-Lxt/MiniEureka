package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/cluster"
	"minieureka/internal/events"
	"minieureka/internal/model"
)

func TestTopologyIncludesUnprobedPeer(t *testing.T) {
	t.Parallel()
	hlc, err := clock.New("node-local")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	members := cluster.NewTable(model.Member{
		NodeID: "node-local", BootID: "boot-local", Status: model.MemberAlive,
		Incarnation: 1, LastSeenAt: now, Version: hlc.Now(),
	}, hlc, time.Second, 2*time.Second, nil)
	peer := model.Member{
		NodeID: "node-remote", BootID: "boot-remote", Status: model.MemberSuspect,
		Incarnation: 1, LastSeenAt: now, Version: model.Version{
			PhysicalMillis: now.Add(time.Millisecond).UnixMilli(), OriginNodeID: "node-observer",
		},
	}
	if _, changed := members.Merge(peer); !changed {
		t.Fatal("suspect peer was not merged")
	}

	server := New(Options{
		NodeID: "node-local", Members: members,
		Events: events.New(16, "node-local", "boot-local"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/topology", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("topology status = %d body=%s", response.Code, response.Body.String())
	}

	var payload struct {
		Data struct {
			Edges []struct {
				TargetNodeID string     `json:"target_node_id"`
				LastSuccess  *time.Time `json:"last_success_at"`
			} `json:"edges"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Edges) != 1 {
		t.Fatalf("topology edges = %#v", payload.Data.Edges)
	}
	edge := payload.Data.Edges[0]
	if edge.TargetNodeID != "node-remote" || edge.LastSuccess != nil {
		t.Fatalf("unprobed peer edge = %#v", edge)
	}
}
