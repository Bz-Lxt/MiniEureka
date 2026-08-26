package model

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCompareVersionTotalOrderAndRoundTrip(t *testing.T) {
	versions := []Version{
		{PhysicalMillis: 10, Logical: 1, OriginNodeID: "node-a"},
		{PhysicalMillis: 10, Logical: 2, OriginNodeID: "node-a"},
		{PhysicalMillis: 10, Logical: 2, OriginNodeID: "node-b"},
		{PhysicalMillis: 11, Logical: 0, OriginNodeID: "node-a"},
	}
	for i := range versions {
		parsed, err := ParseVersion(versions[i].String())
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", versions[i].String(), err)
		}
		if parsed != versions[i] {
			t.Fatalf("round trip got %+v, want %+v", parsed, versions[i])
		}
		for j := range versions {
			comparison := CompareVersion(versions[i], versions[j])
			reverse := CompareVersion(versions[j], versions[i])
			if comparison != -reverse {
				t.Fatalf("comparison is not antisymmetric for %d and %d", i, j)
			}
			if i < j && comparison >= 0 {
				t.Fatalf("version %d should precede %d", i, j)
			}
		}
	}
}

func TestInstanceValidationAndClone(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	record := Instance{
		Service: "orders", InstanceID: "orders-1", RegistrationID: "reg-1",
		Host: "127.0.0.1", Port: 8080, Protocol: ProtocolHTTP,
		Metadata: map[string]string{"zone": "east"}, Status: StatusActive,
		StatusReason: ReasonRegistered, Generation: 1, LeaseID: "lease-1",
		LeaseEpoch:   Version{PhysicalMillis: 1000, OriginNodeID: "node-1"},
		Version:      Version{PhysicalMillis: 1000, OriginNodeID: "node-1"},
		OriginNodeID: "node-1", RegisteredAt: now, LastHeartbeatAt: now, UpdatedAt: now,
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
	clone := record.Clone()
	clone.Metadata["zone"] = "west"
	if record.Metadata["zone"] != "east" {
		t.Fatal("Clone exposed mutable metadata")
	}

	tests := []struct {
		name   string
		mutate func(*Instance)
		field  string
	}{
		{"service path", func(i *Instance) { i.Service = "bad/name" }, "service"},
		{"port", func(i *Instance) { i.Port = 0 }, "port"},
		{"protocol", func(i *Instance) { i.Protocol = "smtp" }, "protocol"},
		{"too much metadata", func(i *Instance) {
			i.Metadata = make(map[string]string)
			for n := 0; n < MaxMetadataItems+1; n++ {
				i.Metadata[string(rune('a'+n))] = "v"
			}
		}, "metadata"},
		{"eviction missing time", func(i *Instance) {
			i.Status, i.StatusReason = StatusEvicted, ReasonTTLExpired
		}, "evicted_at"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := record.Clone()
			test.mutate(&candidate)
			var validation *ValidationError
			if err := candidate.Validate(); !errors.As(err, &validation) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
			if !strings.Contains(validation.Field, test.field) {
				t.Fatalf("field = %q, want containing %q", validation.Field, test.field)
			}
		})
	}
}

func TestMutationStateValidation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	record := Instance{
		Service: "orders", InstanceID: "one", RegistrationID: "reg", Host: "host",
		Port: 80, Protocol: ProtocolHTTP, Metadata: map[string]string{}, Status: StatusActive,
		StatusReason: ReasonRegistered, Generation: 1, LeaseID: "lease",
		LeaseEpoch: Version{PhysicalMillis: 1, OriginNodeID: "node"},
		Version:    Version{PhysicalMillis: 1, OriginNodeID: "node"}, OriginNodeID: "node",
		RegisteredAt: now, LastHeartbeatAt: now, UpdatedAt: now,
	}
	mutation := Mutation{Kind: MutationHeartbeat, Record: record, EventID: "event"}
	if err := mutation.Validate(); err == nil {
		t.Fatal("heartbeat with REGISTERED reason should fail")
	}
	mutation.Record.StatusReason = ReasonHeartbeatOK
	if err := mutation.Validate(); err != nil {
		t.Fatalf("valid heartbeat rejected: %v", err)
	}
}
