package registry

import (
	"sort"

	"minieureka/internal/model"
)

type Counts struct {
	Services  int `json:"services"`
	Instances int `json:"instances"`
	Active    int `json:"active"`
	Delayed   int `json:"delayed"`
	Evicted   int `json:"evicted"`
}

type ServiceSummary struct {
	Name    string `json:"name"`
	Active  int    `json:"active"`
	Delayed int    `json:"delayed"`
	Evicted int    `json:"evicted"`
}

func (r *Registry) Get(service, instanceID string) (model.Instance, bool) {
	s := r.shardFor(service)
	s.mu.RLock()
	instances := s.services[service]
	e := instances[instanceID]
	if e == nil {
		s.mu.RUnlock()
		return model.Instance{}, false
	}
	record := e.cloneRecord()
	s.mu.RUnlock()
	return record, true
}

func (r *Registry) CurrentEventID(service, instanceID string) (string, bool) {
	s := r.shardFor(service)
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.services[service][instanceID]
	if e == nil {
		return "", false
	}
	return e.eventID, true
}

// List returns a stable instance-ID ordered service snapshot. With no status
// arguments it returns all retained records, including full tombstones.
func (r *Registry) List(service string, statuses ...model.InstanceStatus) []model.Instance {
	allowed := make(map[model.InstanceStatus]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	s := r.shardFor(service)
	s.mu.RLock()
	instances := s.services[service]
	result := make([]model.Instance, 0, len(instances))
	for _, e := range instances {
		if len(allowed) > 0 {
			if _, ok := allowed[e.record.Status]; !ok {
				continue
			}
		}
		result = append(result, e.cloneRecord())
	}
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].InstanceID < result[j].InstanceID
	})
	return result
}

func (r *Registry) Discover(service string) []model.Instance {
	return r.List(service, model.StatusActive, model.StatusDelayed)
}

// Snapshot copies one shard at a time and never holds more than one lock. It
// is intentionally not a globally linearizable view in this AP registry.
func (r *Registry) Snapshot() []model.Instance {
	result := make([]model.Instance, 0)
	for i := range r.shards {
		s := &r.shards[i]
		s.mu.RLock()
		for _, instances := range s.services {
			for _, e := range instances {
				result = append(result, e.cloneRecord())
			}
		}
		s.mu.RUnlock()
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Service == result[j].Service {
			return result[i].InstanceID < result[j].InstanceID
		}
		return result[i].Service < result[j].Service
	})
	return result
}

func (r *Registry) SnapshotShard(index int) ([]model.Instance, bool) {
	if index < 0 || index >= len(r.shards) {
		return []model.Instance{}, false
	}
	s := &r.shards[index]
	s.mu.RLock()
	result := make([]model.Instance, 0)
	for _, instances := range s.services {
		for _, e := range instances {
			result = append(result, e.cloneRecord())
		}
	}
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].Service == result[j].Service {
			return result[i].InstanceID < result[j].InstanceID
		}
		return result[i].Service < result[j].Service
	})
	return result, true
}

func (r *Registry) Services() []ServiceSummary {
	counts := make(map[string]ServiceSummary)
	for _, record := range r.Snapshot() {
		summary := counts[record.Service]
		summary.Name = record.Service
		switch record.Status {
		case model.StatusActive:
			summary.Active++
		case model.StatusDelayed:
			summary.Delayed++
		case model.StatusEvicted:
			summary.Evicted++
		}
		counts[record.Service] = summary
	}
	result := make([]ServiceSummary, 0, len(counts))
	for _, summary := range counts {
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *Registry) Counts() Counts {
	services := r.Services()
	result := Counts{Services: len(services)}
	for _, service := range services {
		result.Active += service.Active
		result.Delayed += service.Delayed
		result.Evicted += service.Evicted
	}
	result.Instances = result.Active + result.Delayed + result.Evicted
	return result
}

func (r *Registry) Fences() []Fence {
	result := make([]Fence, 0)
	for i := range r.shards {
		s := &r.shards[i]
		s.mu.RLock()
		for _, fence := range s.fences {
			result = append(result, fence)
		}
		s.mu.RUnlock()
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key.String() < result[j].Key.String()
	})
	return result
}

func (r *Registry) GetFence(key model.Key) (Fence, bool) {
	s := r.shardFor(key.Service)
	s.mu.RLock()
	fence, ok := s.fences[key]
	s.mu.RUnlock()
	return fence, ok
}
