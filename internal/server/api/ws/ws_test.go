package ws

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandler_RejectsNonWebSocket exercises the handler's upgrade-failure path:
// a plain HTTP request can't be upgraded, so the handler returns early without
// touching the engine (safe to pass nil).
func TestHandler_RejectsNonWebSocket(t *testing.T) {
	h := Handler(nil)
	if h == nil {
		t.Fatal("Handler returned nil")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil) // no Upgrade headers
	h(rec, req)
	// A failed upgrade responds with a 4xx and does not panic.
	if rec.Code < 400 {
		t.Errorf("non-WebSocket request should be rejected, got status %d", rec.Code)
	}
}
