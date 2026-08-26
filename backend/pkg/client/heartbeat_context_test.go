package client_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"minieureka/internal/model"
	"minieureka/pkg/client"
)

type traceContextKey struct{}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestHeartbeatPropagatesContext(t *testing.T) {
	t.Parallel()
	const traceID = "trace-heartbeat-42"
	var observedTraceID any
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		observedTraceID = request.Context().Value(traceContextKey{})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":{"service":"orders","instance_id":"orders-1","protocol":"http","metadata":{}},"meta":{}}`)),
		}, nil
	})}
	sdk, err := client.New("http://example.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), traceContextKey{}, traceID), time.Minute)
	defer cancel()
	_, err = sdk.Heartbeat(ctx, model.Instance{Service: "orders", InstanceID: "orders-1", LeaseID: "lease-1"}, "heartbeat-1")
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if observedTraceID != traceID {
		t.Fatalf("transport trace context = %v, want %q", observedTraceID, traceID)
	}
}
