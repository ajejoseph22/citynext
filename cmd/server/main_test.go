package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWithTimeoutSetsJSONContentType verifies the timeout response declares JSON,
// keeping the uniform response contract even though http.TimeoutHandler writes
// its body without setting headers.
func TestWithTimeoutSetsJSONContentType(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	h := withTimeout(slow, time.Millisecond)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/appointments", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}
