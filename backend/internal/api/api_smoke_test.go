package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/cluster"
	"minieureka/internal/events"
	"minieureka/internal/gossip"
	"minieureka/internal/model"
	"minieureka/internal/observe"
	"minieureka/internal/registry"
	"minieureka/internal/service"
)

type smokeFixture struct {
	handler http.Handler
	service *service.Service
}

func newSmokeFixture(t *testing.T) smokeFixture {
	t.Helper()
	store, err := registry.New(8)
	if err != nil {
		t.Fatal(err)
	}
	hlc, err := clock.New("node-1")
	if err != nil {
		t.Fatal(err)
	}
	ring := events.New(128, "node-1", "boot-1")
	svc := service.New(store, hlc, ring, nil, service.Options{NodeID: "node-1", LeaseTTL: 30 * time.Second, DelayedAfter: 15 * time.Second})
	now := time.Now().UTC()
	members := cluster.NewTable(model.Member{NodeID: "node-1", BootID: "boot-1", HTTPAddress: "http://node-1:8080", GossipAddress: "node-1:7946", Status: model.MemberAlive, Incarnation: 1, LastSeenAt: now, Version: hlc.Now()}, hlc, 5*time.Second, 15*time.Second, nil)
	ready := &observe.Readiness{}
	ready.SetHTTP(true)
	ready.SetGossip(true)
	ready.SetWorkers(true)
	metrics := observe.NewMetrics(func() observe.InstanceCounts {
		counts := store.Counts()
		return observe.InstanceCounts{Active: counts.Active, Delayed: counts.Delayed, Evicted: counts.Evicted}
	}, func() map[string]int { return map[string]int{"ALIVE": 1} }, ring.Dropped)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(Options{
		NodeID: "node-1", BootID: "boot-1", DemoMode: true, MaxBodyBytes: 64 << 10,
		Service: svc, Members: members, Events: ring, Faults: gossip.NewFaults(1), Metrics: metrics,
		Readiness: ready, Logger: logger,
		DemoOffline: func(serviceName, instanceID, leaseID, operationID string) (service.OperationResult, error) {
			return svc.Deregister(serviceName, instanceID, leaseID, operationID, true)
		},
	})
	return smokeFixture{handler: server.Handler(), service: svc}
}

func TestAPIRegistrationLifecycleAndDashboard(t *testing.T) {
	t.Parallel()
	fixture := newSmokeFixture(t)
	registration := serveJSON(t, fixture.handler, http.MethodPost, "/api/v1/services/orders/instances", `{"instance_id":"orders-1","registration_id":"reg-1","host":"127.0.0.1","port":9001,"protocol":"http","metadata":{"zone":"a"},"demo":true}`)
	if registration.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", registration.Code, registration.Body.String())
	}
	var registered struct {
		Data model.Instance `json:"data"`
	}
	if err := json.Unmarshal(registration.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	if registered.Data.LeaseID == "" || registered.Data.Status != model.StatusActive {
		t.Fatalf("registered = %#v", registered.Data)
	}
	discovery := serveJSON(t, fixture.handler, http.MethodGet, "/api/v1/services/orders/instances", "")
	if discovery.Code != http.StatusOK || !strings.Contains(discovery.Body.String(), `"instance_id":"orders-1"`) {
		t.Fatalf("discover status=%d body=%s", discovery.Code, discovery.Body.String())
	}
	heartbeatBody, _ := json.Marshal(operationBody{LeaseID: registered.Data.LeaseID, OperationID: "heartbeat-1"})
	heartbeat := serveJSON(t, fixture.handler, http.MethodPut, "/api/v1/services/orders/instances/orders-1/heartbeat", string(heartbeatBody))
	if heartbeat.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", heartbeat.Code, heartbeat.Body.String())
	}
	snapshot := serveJSON(t, fixture.handler, http.MethodGet, "/api/v1/dashboard/snapshot", "")
	if snapshot.Code != http.StatusOK || !strings.Contains(snapshot.Body.String(), `"capabilities"`) || !strings.Contains(snapshot.Body.String(), `"nodes"`) {
		t.Fatalf("snapshot status=%d body=%s", snapshot.Code, snapshot.Body.String())
	}
	topology := serveJSON(t, fixture.handler, http.MethodGet, "/api/v1/cluster/topology", "")
	if topology.Code != http.StatusOK || !strings.Contains(topology.Body.String(), `"edges":[]`) {
		t.Fatalf("topology status=%d body=%s", topology.Code, topology.Body.String())
	}
	metrics := serveJSON(t, fixture.handler, http.MethodGet, "/metrics", "")
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), "minieureka_instances") {
		t.Fatalf("metrics status=%d body=%s", metrics.Code, metrics.Body.String())
	}
	offlineBody, _ := json.Marshal(operationBody{LeaseID: registered.Data.LeaseID, OperationID: "offline-1"})
	offline := serveJSON(t, fixture.handler, http.MethodPost, "/api/v1/demo/services/orders/instances/orders-1/offline", string(offlineBody))
	if offline.Code != http.StatusAccepted || !strings.Contains(offline.Body.String(), `"status":"EVICTED"`) {
		t.Fatalf("offline status=%d body=%s", offline.Code, offline.Body.String())
	}
	discovery = serveJSON(t, fixture.handler, http.MethodGet, "/api/v1/services/orders/instances", "")
	if discovery.Code != http.StatusOK || !strings.Contains(discovery.Body.String(), `"data":[]`) {
		t.Fatalf("post-offline discover status=%d body=%s", discovery.Code, discovery.Body.String())
	}
}

func TestAPIRejectsStaleLeaseAndUnknownFields(t *testing.T) {
	t.Parallel()
	fixture := newSmokeFixture(t)
	bad := serveJSON(t, fixture.handler, http.MethodPost, "/api/v1/services/orders/instances", `{"instance_id":"o1","registration_id":"r1","host":"localhost","port":80,"protocol":"http","unexpected":true}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "invalid_json") {
		t.Fatalf("bad status=%d body=%s", bad.Code, bad.Body.String())
	}
	stale := serveJSON(t, fixture.handler, http.MethodPut, "/api/v1/services/orders/instances/missing/heartbeat", `{"lease_id":"wrong","operation_id":"op1"}`)
	if stale.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", stale.Code, stale.Body.String())
	}
}

func serveJSON(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var input io.Reader
	if body != "" {
		input = bytes.NewBufferString(body)
	}
	request := httptest.NewRequest(method, path, input)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
