package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"minieureka/internal/api"
	"minieureka/internal/events"
)

func TestGossipEventsPreservePublishedPayload(t *testing.T) {
	ring := events.New(8, "node-1", "boot-1")
	published := ring.Publish(events.Event{
		Type:    events.InstanceRegistered,
		EventID: "event-1",
		Payload: events.Payload(map[string]string{"service": "orders"}),
	})

	redacted := events.Payload(map[string]string{"service": "hidden"})
	if len(redacted) != len(published.Payload) {
		t.Fatalf("test payload lengths differ: %d and %d", len(redacted), len(published.Payload))
	}
	copy(published.Payload, redacted)

	server := api.New(api.Options{NodeID: "node-1", BootID: "boot-1", Events: ring})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/gossip/events?cursor=0&event_id=event-1", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("gossip events status = %d, body = %s", response.Code, response.Body.String())
	}

	var body struct {
		Data []struct {
			Payload struct {
				Service string `json:"service"`
			} `json:"payload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode gossip events response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("gossip events count = %d, want 1", len(body.Data))
	}
	if body.Data[0].Payload.Service != "orders" {
		t.Fatalf("stored event service = %q, want %q", body.Data[0].Payload.Service, "orders")
	}
}
