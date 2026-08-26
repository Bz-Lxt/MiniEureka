package model

import (
	"encoding/json"
	"time"
)

type EventType string

const (
	EventInstanceRegistered EventType = "INSTANCE_REGISTERED"
	EventInstanceHeartbeat  EventType = "INSTANCE_HEARTBEAT"
	EventInstanceDelayed    EventType = "INSTANCE_DELAYED"
	EventInstanceEvicted    EventType = "INSTANCE_EVICTED"
	EventInstanceRemoved    EventType = "INSTANCE_REMOVED"
	EventMemberChanged      EventType = "MEMBER_CHANGED"
	EventGossipHop          EventType = "GOSSIP_HOP"
	EventResyncRequired     EventType = "RESYNC_REQUIRED"
)

type DeliveryResult string

const (
	DeliveryApplied   DeliveryResult = "APPLIED"
	DeliveryDuplicate DeliveryResult = "DUPLICATE"
	DeliveryRejected  DeliveryResult = "REJECTED"
)

type Delivery struct {
	AttemptID  string         `json:"attempt_id"`
	SourceNode string         `json:"source_node_id"`
	TargetNode string         `json:"target_node_id"`
	Hop        int            `json:"hop"`
	Result     DeliveryResult `json:"result"`
	LatencyMS  int64          `json:"latency_ms"`
}

type EventEnvelope struct {
	Seq          uint64          `json:"seq"`
	Schema       int             `json:"schema_version"`
	StreamNodeID string          `json:"stream_node_id"`
	StreamBootID string          `json:"stream_boot_id"`
	Type         EventType       `json:"type"`
	EventID      string          `json:"event_id"`
	EntityKey    string          `json:"entity_key"`
	Revision     string          `json:"revision"`
	OriginNodeID string          `json:"origin_node_id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Payload      json.RawMessage `json:"payload"`
	Delivery     *Delivery       `json:"delivery,omitempty"`
}
