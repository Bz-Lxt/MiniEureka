package gossip

import (
	"math/rand"
	"sync"
)

type Fault struct {
	PeerNodeID  string `json:"peer_node_id"`
	DropPercent int    `json:"drop_percent"`
	Blocked     bool   `json:"blocked"`
}

type Faults struct {
	mu     sync.Mutex
	rng    *rand.Rand
	byPeer map[string]Fault
}

func NewFaults(seed int64) *Faults {
	return &Faults{rng: rand.New(rand.NewSource(seed)), byPeer: make(map[string]Fault)}
}

func (f *Faults) Set(fault Fault) Fault {
	if fault.DropPercent < 0 {
		fault.DropPercent = 0
	}
	if fault.DropPercent > 100 {
		fault.DropPercent = 100
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byPeer[fault.PeerNodeID] = fault
	return fault
}

func (f *Faults) Get(peerNodeID string) Fault {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byPeer[peerNodeID]
}

func (f *Faults) ShouldDrop(peerNodeID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	fault := f.byPeer[peerNodeID]
	return fault.Blocked || (fault.DropPercent > 0 && f.rng.Intn(100) < fault.DropPercent)
}
