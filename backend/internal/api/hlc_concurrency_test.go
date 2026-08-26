package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"minieureka/internal/api"
	"minieureka/internal/clock"
	"minieureka/internal/events"
	"minieureka/internal/model"
	"minieureka/internal/registry"
	"minieureka/internal/service"
)

type registrationClockSource struct {
	now           time.Time
	calls         atomic.Int32
	firstEntered  chan struct{}
	secondEntered chan struct{}
	firstDone     <-chan struct{}
}

func newRegistrationClockSource(now time.Time, firstDone <-chan struct{}) *registrationClockSource {
	return &registrationClockSource{
		now:           now,
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		firstDone:     firstDone,
	}
}

func (s *registrationClockSource) Now() time.Time {
	switch s.calls.Add(1) {
	case 1:
		close(s.firstEntered)
		select {
		case <-s.secondEntered:
		case <-time.After(50 * time.Millisecond):
		}
	case 2:
		close(s.secondEntered)
		<-s.firstDone
	}
	return s.now
}

func TestConcurrentRemoteObservationsPreserveVersionOrder(t *testing.T) {
	firstDone := make(chan struct{})
	source := newRegistrationClockSource(time.UnixMilli(10_000), firstDone)
	hlc, err := clock.New("node-local", clock.WithSource(source), clock.WithMaxFutureSkew(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	store, err := registry.New(8)
	if err != nil {
		t.Fatal(err)
	}
	ring := events.New(64, "node-local", "boot-local")
	svc := service.New(store, hlc, ring, nil, service.Options{NodeID: "node-local", LeaseTTL: 30 * time.Second})
	handler := api.New(api.Options{
		NodeID:  "node-local",
		BootID:  "boot-local",
		Service: svc,
		Events:  ring,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Handler()

	remoteMutation := func(serviceName, instanceID, nodeID string, version model.Version) model.Mutation {
		now := time.UnixMilli(10_000).UTC()
		return model.Mutation{
			Kind: model.MutationRegister,
			Record: model.Instance{
				Service: serviceName, InstanceID: instanceID, RegistrationID: "registration-" + instanceID,
				Host: "127.0.0.1", Port: 8080, Protocol: model.ProtocolHTTP, Metadata: map[string]string{},
				Status: model.StatusActive, StatusReason: model.ReasonRegistered, Generation: 1,
				LeaseID: "lease-" + instanceID, LeaseEpoch: version, Version: version, OriginNodeID: nodeID,
				RegisteredAt: now, LastHeartbeatAt: now, UpdatedAt: now,
			},
			EventID: "event-" + instanceID, RemainingTTLMillis: 30_000,
		}
	}
	high := model.Version{PhysicalMillis: 20_000, OriginNodeID: "node-high"}
	low := model.Version{PhysicalMillis: 15_000, OriginNodeID: "node-low"}
	errors := make(chan error, 2)
	go func() {
		defer close(firstDone)
		_, applyErr := svc.ApplyRemote(remoteMutation("inventory", "inventory-high", "node-high", high))
		errors <- applyErr
	}()
	select {
	case <-source.firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first remote observation did not start")
	}
	go func() {
		_, applyErr := svc.ApplyRemote(remoteMutation("catalog", "catalog-low", "node-low", low))
		errors <- applyErr
	}()
	for range 2 {
		if applyErr := <-errors; applyErr != nil {
			t.Fatalf("ApplyRemote() error = %v", applyErr)
		}
	}

	body := `{"instance_id":"orders-local","registration_id":"registration-local","host":"127.0.0.1","port":8080,"protocol":"http","metadata":{}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/services/orders/instances", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data model.Instance `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if model.CompareVersion(payload.Data.Version, high) <= 0 {
		t.Fatalf("local registration version = %s, want later than observed remote version %s", payload.Data.Version.String(), high.String())
	}
}
