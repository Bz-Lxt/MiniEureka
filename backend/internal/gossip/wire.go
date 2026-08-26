package gossip

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const WireVersion = 1

type MessageType string

const (
	MessagePing         MessageType = "ping"
	MessageAck          MessageType = "ack"
	MessageMembers      MessageType = "members"
	MessageDigest       MessageType = "digest"
	MessageDelta        MessageType = "delta"
	MessageSyncRequired MessageType = "sync_required"
	MessageReceipt      MessageType = "receipt"
)

var (
	ErrInvalidSignature = errors.New("invalid gossip signature")
	ErrReplay           = errors.New("replayed gossip message")
	ErrClockSkew        = errors.New("gossip timestamp outside allowed skew")
	ErrWrongCluster     = errors.New("wrong gossip cluster")
	ErrMessageTooLarge  = errors.New("gossip message too large")
	ErrInvalidMessage   = errors.New("invalid gossip message")
)

type Envelope struct {
	Version   int             `json:"version"`
	ClusterID string          `json:"cluster_id"`
	Type      MessageType     `json:"type"`
	MessageID string          `json:"message_id"`
	Sender    string          `json:"sender"`
	SentAt    time.Time       `json:"sent_at"`
	Nonce     string          `json:"nonce"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

type Authenticator struct {
	secret    []byte
	clusterID string
	maxSkew   time.Duration
	maxBytes  int
	now       func() time.Time
	nonces    *nonceCache
}

func NewAuthenticator(secret, clusterID string, maxSkew time.Duration, maxBytes int) *Authenticator {
	if maxSkew <= 0 {
		maxSkew = 30 * time.Second
	}
	if maxBytes <= 0 {
		maxBytes = 1200
	}
	return &Authenticator{
		secret:    []byte(secret),
		clusterID: clusterID,
		maxSkew:   maxSkew,
		maxBytes:  maxBytes,
		now:       time.Now,
		nonces:    newNonceCache(4096),
	}
}

func (a *Authenticator) NewEnvelope(messageType MessageType, sender string, payload any) (Envelope, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal gossip payload: %w", err)
	}
	envelope := Envelope{
		Version:   WireVersion,
		ClusterID: a.clusterID,
		Type:      messageType,
		MessageID: randomID("msg"),
		Sender:    sender,
		SentAt:    a.now().UTC(),
		Nonce:     randomNonce(),
		Payload:   encoded,
	}
	envelope.Signature = a.signature(envelope)
	return envelope, nil
}

func (a *Authenticator) Encode(envelope Envelope) ([]byte, error) {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal gossip envelope: %w", err)
	}
	if len(encoded) > a.maxBytes {
		return nil, ErrMessageTooLarge
	}
	return encoded, nil
}

func (a *Authenticator) DecodeAndVerify(data []byte) (Envelope, error) {
	if len(data) == 0 || len(data) > a.maxBytes {
		return Envelope{}, ErrMessageTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrInvalidMessage, err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Envelope{}, err
	}
	if envelope.Version != WireVersion || envelope.MessageID == "" || envelope.Sender == "" || envelope.Nonce == "" || !validMessageType(envelope.Type) {
		return Envelope{}, ErrInvalidMessage
	}
	if envelope.ClusterID != a.clusterID {
		return Envelope{}, ErrWrongCluster
	}
	delta := a.now().Sub(envelope.SentAt)
	if delta < -a.maxSkew || delta > a.maxSkew {
		return Envelope{}, ErrClockSkew
	}
	expected, err := hex.DecodeString(envelope.Signature)
	if err != nil || !hmac.Equal(expected, mustDecodeHex(a.signature(envelope))) {
		return Envelope{}, ErrInvalidSignature
	}
	if !a.nonces.Add(envelope.Sender+"\x00"+envelope.Nonce, a.now().Add(a.maxSkew)) {
		return Envelope{}, ErrReplay
	}
	return envelope, nil
}

func (a *Authenticator) signature(envelope Envelope) string {
	payloadHash := sha256.Sum256(envelope.Payload)
	canonical := strings.Join([]string{
		strconv.Itoa(envelope.Version), envelope.ClusterID, string(envelope.Type), envelope.MessageID,
		envelope.Sender, envelope.SentAt.UTC().Format(time.RFC3339Nano), envelope.Nonce,
		hex.EncodeToString(payloadHash[:]),
	}, "\n")
	mac := hmac.New(sha256.New, a.secret)
	_, _ = io.WriteString(mac, canonical)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *Authenticator) SignHTTPRequest(request *http.Request, nodeID string, body []byte) {
	timestamp := a.now().UTC().Format(time.RFC3339Nano)
	nonce := randomNonce()
	request.Header.Set("X-MiniEureka-Node-ID", nodeID)
	request.Header.Set("X-MiniEureka-Timestamp", timestamp)
	request.Header.Set("X-MiniEureka-Nonce", nonce)
	request.Header.Set("X-MiniEureka-Signature", a.httpSignature(request.Method, request.URL.Path, nodeID, timestamp, nonce, body))
}

func (a *Authenticator) VerifyHTTPRequest(request *http.Request, body []byte) error {
	nodeID := request.Header.Get("X-MiniEureka-Node-ID")
	timestamp := request.Header.Get("X-MiniEureka-Timestamp")
	nonce := request.Header.Get("X-MiniEureka-Nonce")
	signature := request.Header.Get("X-MiniEureka-Signature")
	if nodeID == "" || timestamp == "" || nonce == "" || signature == "" {
		return ErrInvalidSignature
	}
	sentAt, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return ErrInvalidSignature
	}
	delta := a.now().Sub(sentAt)
	if delta < -a.maxSkew || delta > a.maxSkew {
		return ErrClockSkew
	}
	expected := a.httpSignature(request.Method, request.URL.Path, nodeID, timestamp, nonce, body)
	provided, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(provided, mustDecodeHex(expected)) {
		return ErrInvalidSignature
	}
	if !a.nonces.Add(nodeID+"\x00http\x00"+nonce, a.now().Add(a.maxSkew)) {
		return ErrReplay
	}
	return nil
}

func (a *Authenticator) httpSignature(method, path, nodeID, timestamp, nonce string, body []byte) string {
	hash := sha256.Sum256(body)
	canonical := strings.Join([]string{strings.ToUpper(method), path, a.clusterID, nodeID, timestamp, nonce, hex.EncodeToString(hash[:])}, "\n")
	mac := hmac.New(sha256.New, a.secret)
	_, _ = io.WriteString(mac, canonical)
	return hex.EncodeToString(mac.Sum(nil))
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidMessage
	}
	return nil
}

func validMessageType(messageType MessageType) bool {
	switch messageType {
	case MessagePing, MessageAck, MessageMembers, MessageDigest, MessageDelta, MessageSyncRequired, MessageReceipt:
		return true
	default:
		return false
	}
}

func mustDecodeHex(value string) []byte {
	decoded, _ := hex.DecodeString(value)
	return decoded
}

func randomID(prefix string) string {
	return prefix + "-" + randomNonce()
}

func randomNonce() string {
	var data [12]byte
	if _, err := rand.Read(data[:]); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	}
	return base64.RawURLEncoding.EncodeToString(data[:])
}

type nonceCache struct {
	mu       sync.Mutex
	entries  map[string]time.Time
	capacity int
}

func newNonceCache(capacity int) *nonceCache {
	return &nonceCache{entries: make(map[string]time.Time), capacity: capacity}
}

func (c *nonceCache) Add(key string, expires time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if deadline, ok := c.entries[key]; ok && deadline.After(now) {
		return false
	}
	if len(c.entries) >= c.capacity {
		for existing, deadline := range c.entries {
			if !deadline.After(now) || len(c.entries) >= c.capacity {
				delete(c.entries, existing)
			}
			if len(c.entries) < c.capacity {
				break
			}
		}
	}
	c.entries[key] = expires
	return true
}
