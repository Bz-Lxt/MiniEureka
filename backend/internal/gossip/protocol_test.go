package gossip

import (
	"testing"
	"time"

	"minieureka/internal/model"
	"minieureka/internal/registry"
)

func TestControlPayloadsFitUDPBudget(t *testing.T) {
	t.Parallel()
	auth := NewAuthenticator("secret", "cluster", time.Minute, 1200)
	now := time.Now().UTC()
	member := model.Member{
		NodeID: "registry-node-with-a-realistic-name", BootID: "boot-0123456789abcdef",
		HTTPAddress:   "http://registry-node-with-a-realistic-name:8080",
		GossipAddress: "registry-node-with-a-realistic-name:7946", Status: model.MemberAlive,
		Incarnation: 1, LastSeenAt: now,
		Version: model.Version{PhysicalMillis: now.UnixMilli(), Logical: 12, OriginNodeID: "registry-node-with-a-realistic-name"},
	}
	digests := []registry.ShardDigest{
		{Shard: 1, Revision: 10, Entries: 300, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{Shard: 2, Revision: 11, Entries: 300, SHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"},
	}
	for name, payload := range map[string]any{
		"ping":    PingPayload{Self: member, Members: []model.Member{}, Digests: digests},
		"member":  []model.Member{member},
		"receipt": ReceiptPayload{EventID: "event-0123456789abcdef", AttemptID: "attempt-0123456789abcdef", SourceNodeID: member.NodeID, TargetNodeID: "node-2", Hop: 3, Result: "APPLIED", LatencyMS: 2.5},
	} {
		envelope, err := auth.NewEnvelope(MessagePing, member.NodeID, payload)
		if err != nil {
			t.Fatalf("%s envelope: %v", name, err)
		}
		encoded, err := auth.Encode(envelope)
		if err != nil {
			t.Fatalf("%s does not fit UDP budget: %v", name, err)
		}
		if len(encoded) > 1200 {
			t.Fatalf("%s encoded length = %d", name, len(encoded))
		}
	}
}
