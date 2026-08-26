package gossip

import (
	"testing"
	"time"
)

func FuzzDecodeEnvelope(f *testing.F) {
	auth := NewAuthenticator("secret", "cluster", time.Minute, 1200)
	valid, _ := auth.NewEnvelope(MessagePing, "node-1", map[string]string{"value": "seed"})
	encoded, _ := auth.Encode(valid)
	f.Add(encoded)
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = auth.DecodeAndVerify(data)
	})
}
