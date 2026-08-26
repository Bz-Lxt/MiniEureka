package gossip

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnvelopeAuthenticationAndReplay(t *testing.T) {
	t.Parallel()
	auth := NewAuthenticator("secret", "cluster", 30*time.Second, 1200)
	envelope, err := auth.NewEnvelope(MessagePing, "node-1", map[string]string{"hello": "world"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := auth.Encode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.DecodeAndVerify(encoded); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := auth.DecodeAndVerify(encoded); !errors.Is(err, ErrReplay) {
		t.Fatalf("second verify error = %v", err)
	}
}

func TestEnvelopeRejectsTamperAndOversize(t *testing.T) {
	t.Parallel()
	auth := NewAuthenticator("secret", "cluster", time.Minute, 500)
	envelope, _ := auth.NewEnvelope(MessageDelta, "node-1", map[string]string{"x": string(bytes.Repeat([]byte{'a'}, 1000))})
	if _, err := auth.Encode(envelope); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("Encode error = %v", err)
	}
	auth = NewAuthenticator("secret", "cluster", time.Minute, 1200)
	envelope, _ = auth.NewEnvelope(MessagePing, "node-1", map[string]bool{"ok": true})
	envelope.Sender = "node-2"
	encoded, _ := auth.Encode(envelope)
	if _, err := auth.DecodeAndVerify(encoded); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tampered error = %v", err)
	}
}

func TestHTTPAuthentication(t *testing.T) {
	t.Parallel()
	auth := NewAuthenticator("secret", "cluster", time.Minute, 1200)
	body := []byte(`{"cursor":""}`)
	req := httptest.NewRequest("POST", "/internal/v1/anti-entropy", bytes.NewReader(body))
	auth.SignHTTPRequest(req, "node-1", body)
	if err := auth.VerifyHTTPRequest(req, body); err != nil {
		t.Fatal(err)
	}
}
