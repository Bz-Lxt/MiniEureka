package gossip_test

import (
	"strings"
	"testing"
	"time"

	"minieureka/internal/gossip"
)

func TestAuthenticatorEncodedMessageRemainsStable(t *testing.T) {
	encoder := gossip.NewAuthenticator("secret", "cluster", time.Minute, 4096)
	first, err := encoder.NewEnvelope(gossip.MessagePing, "node-1", map[string]any{
		"sequence": 1,
		"padding":  strings.Repeat("a", 600),
	})
	if err != nil {
		t.Fatalf("create first envelope: %v", err)
	}
	firstEncoded, err := encoder.Encode(first)
	if err != nil {
		t.Fatalf("encode first envelope: %v", err)
	}

	second, err := encoder.NewEnvelope(gossip.MessagePing, "node-1", map[string]any{
		"sequence": 2,
		"padding":  strings.Repeat("b", 600),
	})
	if err != nil {
		t.Fatalf("create second envelope: %v", err)
	}
	if _, err := encoder.Encode(second); err != nil {
		t.Fatalf("encode second envelope: %v", err)
	}

	verifier := gossip.NewAuthenticator("secret", "cluster", time.Minute, 4096)
	decoded, err := verifier.DecodeAndVerify(firstEncoded)
	if err != nil {
		t.Fatalf("first encoded envelope changed after the second encode: %v", err)
	}
	if decoded.MessageID != first.MessageID {
		t.Fatalf("first encoded envelope now contains message %q, want %q", decoded.MessageID, first.MessageID)
	}
}
