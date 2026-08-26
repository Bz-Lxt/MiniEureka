package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"minieureka/internal/model"
)

type ShardDigest struct {
	Shard    int    `json:"shard"`
	Revision uint64 `json:"revision"`
	Entries  int    `json:"entries"`
	SHA256   string `json:"sha256"`
}

// MutationsForShard exports retained authoritative entries for anti-entropy.
// Remaining TTL is computed at export time, never copied from the original
// receive path, so transit and queue time cannot accidentally renew a lease.
func (r *Registry) MutationsForShard(index int) ([]model.Mutation, bool) {
	if index < 0 || index >= len(r.shards) {
		return []model.Mutation{}, false
	}
	now := r.now()
	s := &r.shards[index]
	s.mu.RLock()
	result := make([]model.Mutation, 0)
	for _, instances := range s.services {
		for _, e := range instances {
			remaining := int64(0)
			if e.record.Discoverable() && !e.record.LeaseDeadline.IsZero() {
				remaining = max(e.record.LeaseDeadline.Sub(now).Milliseconds(), int64(0))
			}
			record := e.cloneRecord()
			// DELAYED is a node-local monotonic-clock projection, not an
			// authoritative mutation. Export the underlying live mutation so the
			// receiving node derives delay from remaining TTL on its own clock.
			if record.Status == model.StatusDelayed {
				record.Status = model.StatusActive
				if e.kind == model.MutationRegister {
					record.StatusReason = model.ReasonRegistered
				} else {
					record.StatusReason = model.ReasonHeartbeatOK
				}
			}
			result = append(result, model.Mutation{
				Kind:               e.kind,
				Record:             record,
				EventID:            e.eventID,
				RemainingTTLMillis: remaining,
			})
		}
	}
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].Record.Key().String() < result[j].Record.Key().String()
	})
	return result, true
}

func (r *Registry) Export() []model.Mutation {
	result := make([]model.Mutation, 0)
	for index := range r.shards {
		mutations, _ := r.MutationsForShard(index)
		result = append(result, mutations...)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Record.Key().String() < result[j].Record.Key().String()
	})
	return result
}

type digestEntry struct {
	Key        model.Key          `json:"key"`
	LeaseEpoch model.Version      `json:"lease_epoch"`
	Version    model.Version      `json:"version"`
	Generation uint64             `json:"generation"`
	State      string             `json:"state"`
	Kind       model.MutationKind `json:"kind"`
	EventID    string             `json:"event_id"`
}

func (r *Registry) Digests() []ShardDigest {
	result := make([]ShardDigest, len(r.shards))
	for i := range r.shards {
		result[i], _ = r.Digest(i)
	}
	return result
}

func (r *Registry) Digest(index int) (ShardDigest, bool) {
	if index < 0 || index >= len(r.shards) {
		return ShardDigest{}, false
	}
	s := &r.shards[index]
	s.mu.RLock()
	entries := make([]digestEntry, 0)
	for service, instances := range s.services {
		for instanceID, e := range instances {
			state := "LIVE"
			if e.record.Status == model.StatusEvicted {
				state = string(e.record.StatusReason)
			}
			entries = append(entries, digestEntry{
				Key:        model.Key{Service: service, InstanceID: instanceID},
				LeaseEpoch: e.record.LeaseEpoch,
				Version:    e.record.Version,
				Generation: e.record.Generation,
				State:      state,
				Kind:       e.kind,
				EventID:    e.eventID,
			})
		}
	}
	for key, fence := range s.fences {
		if instances := s.services[key.Service]; instances != nil {
			if _, retained := instances[key.InstanceID]; retained {
				continue
			}
		}
		entries = append(entries, digestEntry{
			Key:        key,
			LeaseEpoch: fence.LeaseEpoch,
			Version:    fence.Version,
			Generation: fence.Generation,
			State:      "FENCE",
			Kind:       fence.Kind,
			EventID:    fence.EventID,
		})
	}
	revision := s.revision
	s.mu.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key.String() < entries[j].Key.String()
	})
	encoded, _ := json.Marshal(entries)
	sum := sha256.Sum256(encoded)
	return ShardDigest{
		Shard:    index,
		Revision: revision,
		Entries:  len(entries),
		SHA256:   hex.EncodeToString(sum[:]),
	}, true
}

// Records returns the currently retained values for keys. Missing values are
// intentionally omitted; callers can consult Fences for compact tombstones.
func (r *Registry) Records(keys []model.Key) []model.Instance {
	result := make([]model.Instance, 0, len(keys))
	for _, key := range keys {
		if record, ok := r.Get(key.Service, key.InstanceID); ok {
			result = append(result, record)
		}
	}
	return result
}
