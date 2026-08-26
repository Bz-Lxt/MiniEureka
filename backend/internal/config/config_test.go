package config

import (
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	cfg := Config{
		NodeID:              "node-1",
		ClusterID:           "cluster-a",
		HTTPAdvertiseAddr:   "http://node-1:8080",
		GossipAddr:          ":7946",
		GossipAdvertiseAddr: "node-1:7946",
		GossipSecret:        "secret",
		LogLevel:            "info",
		ShardCount:          64,
		EventCapacity:       64,
		TickInterval:        time.Second,
		DelayedAfter:        15 * time.Second,
		LeaseTTL:            30 * time.Second,
		EvictedDisplayTTL:   time.Minute,
		TombstoneTTL:        5 * time.Minute,
		Fanout:              3,
		MaxUDPBytes:         1200,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	cfg.ShardCount = 63
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted a non-power-of-two shard count")
	}
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()
	got := splitCSV(" a:1, b:2, a:1 ,, ")
	if len(got) != 2 || got[0] != "a:1" || got[1] != "b:2" {
		t.Fatalf("splitCSV() = %#v", got)
	}
}

func TestLoadFailsFastOnMalformedNumericConfig(t *testing.T) {
	t.Setenv("GOSSIP_SECRET", "secret")
	t.Setenv("REGISTRY_SHARDS", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted malformed REGISTRY_SHARDS")
	}
}
