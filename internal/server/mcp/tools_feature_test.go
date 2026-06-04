package mcp

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/server/mcp/protocol"
)

func TestCEQueryToolHiddenByDefault(t *testing.T) {
	s := New(&config.Config{}, nil)

	if hasTool(s.tools, "ce_query") {
		t.Fatalf("ce_query should not be registered unless features.ce_query is enabled")
	}
	if !hasTool(s.tools, "ce_search") {
		t.Fatalf("ce_search should remain registered")
	}
	for _, name := range []string{"ce_investigate", "ce_source_ranges", "ce_related_tests", "ce_entrypoints", "ce_lifecycle"} {
		if !hasTool(s.tools, name) {
			t.Fatalf("%s should be registered as a deterministic direct tool", name)
		}
	}
}

func TestCEQueryToolRegisteredWhenFeatureEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Features.CEQuery = true

	s := New(cfg, nil)

	if !hasTool(s.tools, "ce_query") {
		t.Fatalf("ce_query should be registered when features.ce_query is enabled")
	}
}

func hasTool(tools []protocol.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
