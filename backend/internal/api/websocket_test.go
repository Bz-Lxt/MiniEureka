package api

import (
	"net/http/httptest"
	"testing"
)

func TestOriginAllowed(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("GET", "http://example.test/api/v1/events/ws", nil)
	request.Host = "example.test"
	request.Header.Set("Origin", "http://example.test")
	if !originAllowed(request, nil) {
		t.Fatal("same origin rejected")
	}
	request.Header.Set("Origin", "https://evil.test")
	if originAllowed(request, nil) {
		t.Fatal("foreign origin accepted")
	}
	request.Header.Set("Origin", "http://allowed.test")
	if !originAllowed(request, []string{"http://allowed.test"}) {
		t.Fatal("allowlisted origin rejected")
	}
}
