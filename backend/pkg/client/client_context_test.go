package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"minieureka/pkg/client"
)

func TestDiscoverPropagatesCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	releaseRequest := make(chan struct{})
	httpClient := &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
		close(requestStarted)
		select {
		case <-request.Context().Done():
			close(requestCanceled)
			return nil, request.Context().Err()
		case <-releaseRequest:
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
		}, nil
	})}
	defer close(releaseRequest)

	sdk, err := client.New("http://registry.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	result := make(chan error, 1)
	go func() {
		_, discoverErr := sdk.Discover(ctx, "orders")
		result <- discoverErr
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("Discover request did not reach the server")
	}
	cancel()

	select {
	case discoverErr := <-result:
		if !errors.Is(discoverErr, context.Canceled) {
			t.Fatalf("Discover error = %v, want context.Canceled", discoverErr)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Discover did not return after its context was canceled")
	}
	select {
	case <-requestCanceled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("server request context was not canceled")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (function transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
