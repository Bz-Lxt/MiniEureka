package gossip

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"minieureka/internal/clock"
	"minieureka/internal/cluster"
	"minieureka/internal/events"
	"minieureka/internal/model"
	"minieureka/internal/registry"
)

func TestValidDatagramDoesNotDirectlyMakeSeededNodeReady(t *testing.T) {
	t.Parallel()
	store, table, ring := readinessFixtures(t)
	auth := NewAuthenticator("secret", "cluster", time.Minute, 1200)
	engine := NewEngine(
		EngineConfig{NodeID: "node-1", AdvertiseAddress: "node-1:7946", Seeds: []string{"node-2:7946"}},
		auth, nil, NewSelector(1), table, store, nil, ring, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if engine.Ready() {
		t.Fatal("seeded node started ready")
	}
	envelope, err := auth.NewEnvelope(MessageMembers, "node-2", []model.Member{})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := auth.Encode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	engine.handleDatagram(context.Background(), packet, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7946})
	if engine.Ready() {
		t.Fatal("a valid datagram bypassed initial anti-entropy")
	}
}

func TestSyncResultControlsReadiness(t *testing.T) {
	t.Parallel()
	engine := &Engine{}
	if engine.recordSyncResult(errors.New("peer unavailable")) || engine.Ready() {
		t.Fatal("failed sync marked node ready")
	}
	if !engine.recordSyncResult(nil) || !engine.Ready() {
		t.Fatal("successful complete sync did not mark node ready")
	}
}

func TestInitialPeerRequiresSyncEvenWhenDigestMatches(t *testing.T) {
	t.Parallel()
	store, _, _ := readinessFixtures(t)
	engine := &Engine{registry: store}
	if !engine.needsSync(store.Digests()) {
		t.Fatal("initial matching digest did not require a complete sync round")
	}
	engine.recordSyncResult(nil)
	if engine.needsSync(store.Digests()) {
		t.Fatal("ready node requested sync for identical digests")
	}
}

func TestSeedlessNodeBootstrapsReady(t *testing.T) {
	t.Parallel()
	engine := NewEngine(EngineConfig{NodeID: "node-1"}, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if !engine.Ready() {
		t.Fatal("seedless first node did not bootstrap ready")
	}
}

func readinessFixtures(t *testing.T) (*registry.Registry, *cluster.Table, *events.Ring) {
	t.Helper()
	store, err := registry.New(8)
	if err != nil {
		t.Fatal(err)
	}
	hlc, err := clock.New("node-1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	self := model.Member{
		NodeID: "node-1", BootID: "boot-1", HTTPAddress: "http://node-1:8080", GossipAddress: "node-1:7946",
		Status: model.MemberAlive, Incarnation: 1, LastSeenAt: now, Version: hlc.Now(),
	}
	table := cluster.NewTable(self, hlc, 5*time.Second, 15*time.Second, nil)
	return store, table, events.New(64, "node-1", "boot-1")
}
