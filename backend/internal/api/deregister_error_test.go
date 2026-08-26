package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"minieureka/internal/api"
	"minieureka/internal/clock"
	"minieureka/internal/events"
	"minieureka/internal/registry"
	"minieureka/internal/service"
)

func TestDeregisterDoesNotTreatRejectedMutationAsDuplicate(t *testing.T) {
	store, err := registry.New(8)
	if err != nil {
		t.Fatal(err)
	}
	hlc, err := clock.New("node-test")
	if err != nil {
		t.Fatal(err)
	}
	eventRing := events.New(64, "node-test", "boot-test")
	registryService := service.New(store, hlc, eventRing, nil, service.Options{NodeID: "node-test"})
	server := api.New(api.Options{
		NodeID: "node-test", BootID: "boot-test", Service: registryService,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	registration := httptest.NewRequest(http.MethodPost, "/api/v1/services/orders/instances",
		strings.NewReader(`{"instance_id":"orders-1","registration_id":"reg-1","host":"worker-1","port":8080,"protocol":"http"}`))
	registration.Header.Set("Content-Type", "application/json")
	registeredResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(registeredResponse, registration)
	if registeredResponse.Code != http.StatusCreated {
		t.Fatalf("register status = %d; body=%s", registeredResponse.Code, registeredResponse.Body.String())
	}
	var registered struct {
		Data struct {
			LeaseID string `json:"lease_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(registeredResponse.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}

	query := url.Values{"lease_id": {registered.Data.LeaseID}, "operation_id": {"offline/request-1"}}
	deregistration := httptest.NewRequest(http.MethodDelete,
		"/api/v1/services/orders/instances/orders-1?"+query.Encode(), nil)
	deregisteredResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(deregisteredResponse, deregistration)
	if deregisteredResponse.Code != http.StatusUnprocessableEntity {
		t.Errorf("deregister status = %d, want %d; event_id=%q body=%s",
			deregisteredResponse.Code, http.StatusUnprocessableEntity,
			deregisteredResponse.Header().Get("X-MiniEureka-Event-ID"), deregisteredResponse.Body.String())
	} else {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Details []struct {
					Field string `json:"field"`
					Code  string `json:"code"`
				} `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(deregisteredResponse.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Error.Code != "validation_error" || len(payload.Error.Details) != 1 ||
			payload.Error.Details[0].Field != "operation_id" || payload.Error.Details[0].Code != "invalid_format" {
			t.Errorf("deregister error = %#v", payload.Error)
		}
	}

	discovery := httptest.NewRequest(http.MethodGet, "/api/v1/services/orders/instances", nil)
	discoveryResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(discoveryResponse, discovery)
	if discoveryResponse.Code != http.StatusOK ||
		!strings.Contains(discoveryResponse.Body.String(), `"instance_id":"orders-1"`) ||
		!strings.Contains(discoveryResponse.Body.String(), `"status":"ACTIVE"`) {
		t.Fatalf("discover status = %d; body=%s", discoveryResponse.Code, discoveryResponse.Body.String())
	}
}
