package model

import "time"

type MemberStatus string

const (
	MemberAlive   MemberStatus = "ALIVE"
	MemberSuspect MemberStatus = "SUSPECT"
	MemberDead    MemberStatus = "DEAD"
)

func (s MemberStatus) Valid() bool {
	switch s {
	case MemberAlive, MemberSuspect, MemberDead:
		return true
	default:
		return false
	}
}

func (s MemberStatus) Priority() int {
	switch s {
	case MemberDead:
		return 3
	case MemberSuspect:
		return 2
	case MemberAlive:
		return 1
	default:
		return 0
	}
}

type Member struct {
	NodeID        string       `json:"node_id"`
	BootID        string       `json:"boot_id"`
	HTTPAddress   string       `json:"http_address"`
	GossipAddress string       `json:"gossip_address"`
	Status        MemberStatus `json:"status"`
	Incarnation   uint64       `json:"incarnation"`
	LastSeenAt    time.Time    `json:"last_seen_at"`
	Version       Version      `json:"version"`
}

type TopologyEdge struct {
	SourceNodeID string     `json:"source_node_id"`
	TargetNodeID string     `json:"target_node_id"`
	State        string     `json:"state"`
	LastSuccess  *time.Time `json:"last_success_at"`
	LatencyMS    int64      `json:"latency_ms"`
}
