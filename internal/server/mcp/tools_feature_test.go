package mcp

import (
	"encoding/json"
	"testing"

	"github.com/atheory-ai/context-engine/internal/buildinfo"
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

func TestInitializeReportsBuildVersion(t *testing.T) {
	s := New(&config.Config{}, nil)
	response := s.handleInitialize(protocol.Request{ID: "initialize"})

	var result protocol.InitializeResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if result.ServerInfo.Version != buildinfo.Version {
		t.Fatalf("server version = %q, want %q", result.ServerInfo.Version, buildinfo.Version)
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
