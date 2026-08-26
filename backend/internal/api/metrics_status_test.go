package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"minieureka/internal/api"
	"minieureka/internal/observe"
)

func TestMetricsRecordFinalHTTPStatus(t *testing.T) {
	metrics := observe.NewMetrics(nil, nil, nil)
	handler := api.New(api.Options{Metrics: metrics}).Handler()

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing endpoint status = %d, want %d", missing.Code, http.StatusNotFound)
	}

	recorded := httptest.NewRecorder()
	handler.ServeHTTP(recorded, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	want := `minieureka_api_requests_total{method="GET",status="404"} 1`
	if !strings.Contains(recorded.Body.String(), want) {
		t.Fatalf("metrics do not contain %q:\n%s", want, recorded.Body.String())
	}
}
