package registry

import (
	"time"

	"minieureka/internal/model"
)

// MarkDelayed applies the local, non-authoritative health projection only when
// the scheduled lease identity and version still match. It does not create a
// replicated version and cannot change an evicted record.
func (r *Registry) MarkDelayed(key model.Key, leaseID string, expectedVersion model.Version, at time.Time) (model.Instance, bool) {
	s := r.shardFor(key.Service)
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.services[key.Service][key.InstanceID]
	if e == nil || e.record.LeaseID != leaseID ||
		model.CompareVersion(e.record.Version, expectedVersion) != 0 ||
		e.record.Status != model.StatusActive {
		return model.Instance{}, false
	}
	e.record.Status = model.StatusDelayed
	e.record.StatusReason = model.ReasonHeartbeatDelayed
	e.record.UpdatedAt = at
	s.revision++
	return e.cloneRecord(), true
}

// Expired returns conditional TTL candidates. The caller creates HLC-versioned
// TTL_EXPIRE mutations and Apply performs the final conflict check.
func (r *Registry) Expired(now time.Time) []model.Instance {
	result := make([]model.Instance, 0)
	for i := range r.shards {
		s := &r.shards[i]
		s.mu.RLock()
		for _, instances := range s.services {
			for _, e := range instances {
				if e.record.Discoverable() && !e.record.LeaseDeadline.IsZero() &&
					!e.record.LeaseDeadline.After(now) {
					result = append(result, e.cloneRecord())
				}
			}
		}
		s.mu.RUnlock()
	}
	return result
}

// GC removes full tombstone payloads after retention while deliberately
// retaining compact version fences for the life of the process.
func (r *Registry) GC(now time.Time, retention time.Duration) []model.Key {
	removed := make([]model.Key, 0)
	for i := range r.shards {
		s := &r.shards[i]
		s.mu.Lock()
		for service, instances := range s.services {
			for instanceID, e := range instances {
				if e.record.Status != model.StatusEvicted || e.record.EvictedAt == nil ||
					now.Sub(*e.record.EvictedAt) < retention {
					continue
				}
				key := model.Key{Service: service, InstanceID: instanceID}
				delete(instances, instanceID)
				removed = append(removed, key)
				s.revision++
			}
			if len(instances) == 0 {
				delete(s.services, service)
			}
		}
		s.mu.Unlock()
	}
	return removed
}
