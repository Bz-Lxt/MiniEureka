package api_test

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

	"minieureka/internal/api"
	"minieureka/internal/clock"
	"minieureka/internal/demo"
	"minieureka/internal/events"
	"minieureka/internal/registry"
	"minieureka/internal/service"
)

func TestDemoOfflineWithoutSeedEvictsRegisteredInstance(t *testing.T) {
	store, err := registry.New(8)
	if err != nil {
		t.Fatal(err)
	}
	hlc, err := clock.New("node-1")
	if err != nil {
		t.Fatal(err)
	}
	ring := events.New(64, "node-1", "boot-1")
	svc := service.New(store, hlc, ring, nil, service.Options{
		NodeID: "node-1", LeaseTTL: 30 * time.Second, DelayedAfter: 15 * time.Second,
	})
	manager := demo.New(svc, demo.Options{Enabled: true, Seed: false})
	server := api.New(api.Options{
		NodeID: "node-1", BootID: "boot-1", DemoMode: true, Service: svc,
		DemoOffline: manager.Offline,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	registration := request(t, server.Handler(), http.MethodPost, "/api/v1/services/orders/instances",
		`{"instance_id":"orders-demo-1","registration_id":"registration-1","host":"127.0.0.1","port":8080,"protocol":"http","metadata":{},"demo":true}`)
	if registration.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", registration.Code, registration.Body.String())
	}
	var registered struct {
		Data struct {
			LeaseID string `json:"lease_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(registration.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}

	offline := request(t, server.Handler(), http.MethodPost, "/api/v1/demo/services/orders/instances/orders-demo-1/offline",
		`{"lease_id":"`+registered.Data.LeaseID+`","operation_id":"offline-1"}`)
	if offline.Code != http.StatusAccepted || !strings.Contains(offline.Body.String(), `"status":"EVICTED"`) {
		t.Errorf("offline status = %d, body = %s", offline.Code, offline.Body.String())
	}

	discovery := request(t, server.Handler(), http.MethodGet, "/api/v1/services/orders/instances", "")
	if discovery.Code != http.StatusOK || !strings.Contains(discovery.Body.String(), `"data":[]`) {
		t.Fatalf("discover status = %d, body = %s", discovery.Code, discovery.Body.String())
	}
}

func request(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var input io.Reader
	if body != "" {
		input = bytes.NewBufferString(body)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, input))
	return recorder
}
