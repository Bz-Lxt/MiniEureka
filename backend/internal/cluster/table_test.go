package cluster

import (
	"testing"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/model"
)

func TestMemberMergeAndFailureDetection(t *testing.T) {
	t.Parallel()
	hlc, err := clock.New("node-1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	self := model.Member{NodeID: "node-1", BootID: "boot-1", HTTPAddress: "http://n1:8080", GossipAddress: "n1:7946", Status: model.MemberAlive, Incarnation: 1, LastSeenAt: now, Version: hlc.Now()}
	table := NewTable(self, hlc, time.Second, 2*time.Second, nil)
	peer := model.Member{NodeID: "node-2", BootID: "boot-2", HTTPAddress: "http://n2:8080", GossipAddress: "n2:7946", Status: model.MemberAlive, Incarnation: 1, LastSeenAt: now, Version: model.Version{PhysicalMillis: now.UnixMilli(), OriginNodeID: "node-2"}}
	if _, changed := table.Merge(peer); !changed {
		t.Fatal("peer was not merged")
	}
	table.Tick(now.Add(1500 * time.Millisecond))
	got, _ := table.Get("node-2")
	if got.Status != model.MemberSuspect {
		t.Fatalf("status = %s", got.Status)
	}
	table.Tick(now.Add(3 * time.Second))
	got, _ = table.Get("node-2")
	if got.Status != model.MemberDead {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestSelfRefutesSuspicion(t *testing.T) {
	t.Parallel()
	hlc, _ := clock.New("node-1")
	now := time.Now().UTC()
	self := model.Member{NodeID: "node-1", BootID: "boot-1", Status: model.MemberAlive, Incarnation: 1, LastSeenAt: now, Version: hlc.Now()}
	table := NewTable(self, hlc, time.Second, 2*time.Second, nil)
	suspicion := self
	suspicion.Status = model.MemberSuspect
	suspicion.Incarnation = 1
	suspicion.Version = model.Version{PhysicalMillis: now.Add(time.Millisecond).UnixMilli(), OriginNodeID: "node-2"}
	got, changed := table.Merge(suspicion)
	if !changed || got.Status != model.MemberAlive || got.Incarnation != 2 {
		t.Fatalf("refutation = %#v, changed=%v", got, changed)
	}
}
