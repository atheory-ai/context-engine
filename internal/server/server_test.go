package server

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
)

func TestNew(t *testing.T) {
	s := New(&config.Config{}, nil)
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.stopCh == nil {
		t.Error("stop channel not initialized")
	}
	// Stop must be safe to call on a server that was never started.
	s.Stop()
}
