package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"minieureka/internal/model"
)

func TestDiscoverEmptyArray(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"data":[],"meta":{"total":0}}`), nil
	})}
	client, err := New("http://example.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	instances, err := client.Discover(context.Background(), "orders")
	if err != nil || instances == nil || len(instances) != 0 {
		t.Fatalf("Discover() = %#v, %v", instances, err)
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s", request.Method)
		}
		return jsonResponse(`{"data":{"service":"orders","instance_id":"o1","protocol":"http","metadata":{}},"meta":{}}`), nil
	})}
	client, _ := New("http://example.test", httpClient)
	instance, err := client.Register(context.Background(), "orders", RegisterRequest{InstanceID: "o1", Protocol: model.ProtocolHTTP})
	if err != nil || instance.InstanceID != "o1" {
		t.Fatalf("Register() = %#v, %v", instance, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
