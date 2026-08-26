package gossip

import (
	"minieureka/internal/model"
	"minieureka/internal/registry"
)

type PingPayload struct {
	Self    model.Member           `json:"self"`
	Members []model.Member         `json:"members"`
	Digests []registry.ShardDigest `json:"digests"`
}

type AckPayload struct {
	ReplyTo string                 `json:"reply_to"`
	Self    model.Member           `json:"self"`
	Members []model.Member         `json:"members"`
	Digests []registry.ShardDigest `json:"digests"`
}

type DeltaPayload struct {
	Mutation           model.Mutation `json:"mutation"`
	Hop                int            `json:"hop"`
	Trace              bool           `json:"trace"`
	TraceOriginNodeID  string         `json:"trace_origin_node_id,omitempty"`
	TraceOriginAddress string         `json:"trace_origin_address,omitempty"`
}

type ReceiptPayload struct {
	EventID      string  `json:"event_id"`
	AttemptID    string  `json:"attempt_id"`
	SourceNodeID string  `json:"source_node_id"`
	TargetNodeID string  `json:"target_node_id"`
	Hop          int     `json:"hop"`
	Result       string  `json:"result"`
	LatencyMS    float64 `json:"latency_ms"`
}

type AntiEntropyRequest struct {
	Digests []registry.ShardDigest `json:"digests"`
	Cursor  int                    `json:"cursor"`
}

type AntiEntropyResponse struct {
	Mutations  []model.Mutation       `json:"mutations"`
	Fences     []registry.Fence       `json:"fences"`
	Digests    []registry.ShardDigest `json:"digests"`
	NextCursor *int                   `json:"next_cursor"`
}
