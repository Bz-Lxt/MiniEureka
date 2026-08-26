package gossip

import (
	"math/rand"
	"sync"
)

type Selector struct {
	mu  sync.Mutex
	rng *rand.Rand
}

func NewSelector(seed int64) *Selector {
	return &Selector{rng: rand.New(rand.NewSource(seed))}
}

// Pick samples peers without replacement. Input order does not affect safety;
// tests inject a fixed seed for reproducibility.
func (s *Selector) Pick(peers []string, count int) []string {
	if count <= 0 || len(peers) == 0 {
		return []string{}
	}
	if count > len(peers) {
		count = len(peers)
	}
	copyPeers := append([]string(nil), peers...)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range count {
		j := i + s.rng.Intn(len(copyPeers)-i)
		copyPeers[i], copyPeers[j] = copyPeers[j], copyPeers[i]
	}
	return copyPeers[:count]
}
