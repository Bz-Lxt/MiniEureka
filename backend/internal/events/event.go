package events

import (
	"encoding/json"
	"time"
)

const SchemaVersion = 1

type Type string

const (
	InstanceRegistered Type = "INSTANCE_REGISTERED"
	InstanceHeartbeat  Type = "INSTANCE_HEARTBEAT"
	InstanceDelayed    Type = "INSTANCE_DELAYED"
	InstanceEvicted    Type = "INSTANCE_EVICTED"
	InstanceRemoved    Type = "INSTANCE_REMOVED"
	MemberChanged      Type = "MEMBER_CHANGED"
	GossipHop          Type = "GOSSIP_HOP"
	Connected          Type = "CONNECTED"
	ResyncRequired     Type = "RESYNC_REQUIRED"
)

type Delivery struct {
	AttemptID  string  `json:"attempt_id"`
	SourceNode string  `json:"source_node_id"`
	TargetNode string  `json:"target_node_id"`
	Hop        int     `json:"hop"`
	Result     string  `json:"result"`
	LatencyMS  float64 `json:"latency_ms"`
}

// Event is the immutable wire representation retained by Ring.
type Event struct {
	Seq           uint64          `json:"seq"`
	SchemaVersion int             `json:"schema_version"`
	StreamNodeID  string          `json:"stream_node_id"`
	StreamBootID  string          `json:"stream_boot_id"`
	Type          Type            `json:"type"`
	EventID       string          `json:"event_id"`
	EntityKey     string          `json:"entity_key,omitempty"`
	Revision      string          `json:"revision,omitempty"`
	OriginNodeID  string          `json:"origin_node_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
	Delivery      *Delivery       `json:"delivery,omitempty"`
}

func Payload(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func cloneEvent(event Event) Event {
	event.Payload = append(json.RawMessage(nil), event.Payload...)
	if event.Delivery != nil {
		delivery := *event.Delivery
		event.Delivery = &delivery
	}
	return event
}
