package observe

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsHandler(t *testing.T) {
	t.Parallel()
	metrics := NewMetrics(
		func() InstanceCounts { return InstanceCounts{Active: 2, Delayed: 1} },
		func() map[string]int { return map[string]int{"ALIVE": 3} },
		func() uint64 { return 4 },
	)
	metrics.ObserveAPI("GET", 200, 10*time.Millisecond)
	metrics.GossipReceived()
	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	for _, expected := range []string{"minieureka_api_requests_total", `status="ACTIVE"} 2`, "minieureka_gossip_received_total 1", "minieureka_event_dropped_total 4"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
}
