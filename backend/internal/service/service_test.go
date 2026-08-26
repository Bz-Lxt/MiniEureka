package service

import (
	"testing"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/events"
	"minieureka/internal/model"
	"minieureka/internal/registry"
	"minieureka/internal/ttl"
)

func newTestService(t *testing.T) (*Service, *time.Time, *ttl.Wheel) {
	t.Helper()
	now := time.Unix(1000, 0).UTC()
	store, err := registry.New(8, registry.WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	hlc, err := clock.New("node-1")
	if err != nil {
		t.Fatal(err)
	}
	ring := events.New(64, "node-1", "boot-1")
	var svc *Service
	wheel := ttl.New(64, time.Second, func() time.Time { return now }, func(tasks []ttl.Task) { svc.HandleTasks(tasks) })
	svc = New(store, hlc, ring, wheel, Options{NodeID: "node-1", LeaseTTL: 30 * time.Second, DelayedAfter: 15 * time.Second, TombstoneTTL: 5 * time.Minute, Now: func() time.Time { return now }})
	return svc, &now, wheel
}

func TestRegistrationHeartbeatAndDeregister(t *testing.T) {
	t.Parallel()
	svc, _, _ := newTestService(t)
	registered, err := svc.Register(RegisterRequest{Service: "orders", InstanceID: "o1", RegistrationID: "reg-1", Host: "127.0.0.1", Port: 8080, Protocol: model.ProtocolHTTP})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := svc.Register(RegisterRequest{Service: "orders", InstanceID: "o1", RegistrationID: "reg-1", Host: "127.0.0.1", Port: 8080, Protocol: model.ProtocolHTTP})
	if err != nil || !retry.Duplicate || retry.Record.LeaseID != registered.Record.LeaseID || retry.EventID != registered.EventID {
		t.Fatalf("retry = %#v, err=%v", retry, err)
	}
	heartbeat, err := svc.Heartbeat("orders", "o1", registered.Record.LeaseID, "op-1")
	if err != nil || heartbeat.Record.Status != model.StatusActive {
		t.Fatalf("heartbeat = %#v, err=%v", heartbeat, err)
	}
	if _, err := svc.Deregister("orders", "o1", registered.Record.LeaseID, "op-2", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Heartbeat("orders", "o1", registered.Record.LeaseID, "op-3"); err != ErrStaleLease {
		t.Fatalf("heartbeat after deregister error = %v", err)
	}
}

func TestTimeWheelTransitions(t *testing.T) {
	t.Parallel()
	svc, now, wheel := newTestService(t)
	registered, err := svc.Register(RegisterRequest{Service: "orders", InstanceID: "o1", RegistrationID: "reg-1", Host: "127.0.0.1", Port: 8080, Protocol: model.ProtocolHTTP})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(16 * time.Second)
	wheel.Advance(*now)
	record, _ := svc.Registry().Get("orders", "o1")
	if record.Status != model.StatusDelayed {
		t.Fatalf("status = %s, lease=%s", record.Status, registered.Record.LeaseID)
	}
	*now = now.Add(15 * time.Second)
	wheel.Advance(*now)
	record, _ = svc.Registry().Get("orders", "o1")
	if record.Status != model.StatusEvicted || record.StatusReason != model.ReasonTTLExpired {
		t.Fatalf("record = %#v", record)
	}
}
