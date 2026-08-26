package observe

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type InstanceCounts struct {
	Active  int
	Delayed int
	Evicted int
}

type CountProvider func() InstanceCounts
type MemberCountProvider func() map[string]int
type EventDropProvider func() uint64

type Metrics struct {
	mu              sync.RWMutex
	apiRequests     map[string]uint64
	apiLatencyNanos map[string]uint64
	apiLatencyCount map[string]uint64
	gossipSent      atomic.Uint64
	gossipReceived  atomic.Uint64
	gossipRejected  atomic.Uint64
	instanceCounts  CountProvider
	memberCounts    MemberCountProvider
	eventDrops      EventDropProvider
	started         time.Time
}

func NewMetrics(instances CountProvider, members MemberCountProvider, eventDrops EventDropProvider) *Metrics {
	return &Metrics{
		apiRequests:     make(map[string]uint64),
		apiLatencyNanos: make(map[string]uint64),
		apiLatencyCount: make(map[string]uint64),
		instanceCounts:  instances,
		memberCounts:    members,
		eventDrops:      eventDrops,
		started:         time.Now(),
	}
}

func (m *Metrics) ObserveAPI(method string, status int, duration time.Duration) {
	key := strings.ToUpper(method) + "\x00" + fmt.Sprintf("%d", status)
	m.mu.Lock()
	m.apiRequests[key]++
	m.apiLatencyNanos[key] += uint64(max(duration, 0))
	m.apiLatencyCount[key]++
	m.mu.Unlock()
}

func (m *Metrics) GossipSent()     { m.gossipSent.Add(1) }
func (m *Metrics) GossipReceived() { m.gossipReceived.Add(1) }
func (m *Metrics) GossipRejected() { m.gossipRejected.Add(1) }

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.write(response)
	})
}

func (m *Metrics) write(output io.Writer) {
	m.mu.RLock()
	keys := make([]string, 0, len(m.apiRequests))
	for key := range m.apiRequests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	requests := make(map[string]uint64, len(keys))
	latency := make(map[string]uint64, len(keys))
	latencyCount := make(map[string]uint64, len(keys))
	for _, key := range keys {
		requests[key] = m.apiRequests[key]
		latency[key] = m.apiLatencyNanos[key]
		latencyCount[key] = m.apiLatencyCount[key]
	}
	m.mu.RUnlock()
	_, _ = io.WriteString(output, "# TYPE minieureka_api_requests_total counter\n")
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		_, _ = fmt.Fprintf(output, "minieureka_api_requests_total{method=%q,status=%q} %d\n", parts[0], parts[1], requests[key])
		_, _ = fmt.Fprintf(output, "minieureka_api_request_duration_seconds_sum{method=%q,status=%q} %.9f\n", parts[0], parts[1], float64(latency[key])/float64(time.Second))
		_, _ = fmt.Fprintf(output, "minieureka_api_request_duration_seconds_count{method=%q,status=%q} %d\n", parts[0], parts[1], latencyCount[key])
	}
	counts := InstanceCounts{}
	if m.instanceCounts != nil {
		counts = m.instanceCounts()
	}
	_, _ = io.WriteString(output, "# TYPE minieureka_instances gauge\n")
	_, _ = fmt.Fprintf(output, "minieureka_instances{status=\"ACTIVE\"} %d\n", counts.Active)
	_, _ = fmt.Fprintf(output, "minieureka_instances{status=\"DELAYED\"} %d\n", counts.Delayed)
	_, _ = fmt.Fprintf(output, "minieureka_instances{status=\"EVICTED\"} %d\n", counts.Evicted)
	memberCounts := map[string]int{}
	if m.memberCounts != nil {
		memberCounts = m.memberCounts()
	}
	_, _ = io.WriteString(output, "# TYPE minieureka_members gauge\n")
	for _, status := range []string{"ALIVE", "SUSPECT", "DEAD"} {
		_, _ = fmt.Fprintf(output, "minieureka_members{status=%q} %d\n", status, memberCounts[status])
	}
	_, _ = fmt.Fprintf(output, "minieureka_gossip_sent_total %d\n", m.gossipSent.Load())
	_, _ = fmt.Fprintf(output, "minieureka_gossip_received_total %d\n", m.gossipReceived.Load())
	_, _ = fmt.Fprintf(output, "minieureka_gossip_rejected_total %d\n", m.gossipRejected.Load())
	drops := uint64(0)
	if m.eventDrops != nil {
		drops = m.eventDrops()
	}
	_, _ = fmt.Fprintf(output, "minieureka_event_dropped_total %d\n", drops)
	_, _ = fmt.Fprintf(output, "minieureka_go_goroutines %d\n", runtime.NumGoroutine())
	_, _ = fmt.Fprintf(output, "minieureka_process_uptime_seconds %.3f\n", time.Since(m.started).Seconds())
}
