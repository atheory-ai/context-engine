package runner

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/plugins"
)

// PluginRulePacks is best-effort: with a valid but plugin-less data dir it
// returns no packs and a callable cleanup, so the CLI verify path falls back to
// the built-in defaults rather than erroring.
func TestPluginRulePacks_NoPluginsIsEmpty(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	ch := core.NewAppChannels()

	packs, cleanup := PluginRulePacks(context.Background(), cfg, &ch)
	if cleanup == nil {
		t.Fatal("cleanup must be non-nil and safe to call")
	}
	defer cleanup()
	if len(packs) != 0 {
		t.Fatalf("expected no rule packs from a plugin-less data dir, got %d", len(packs))
	}
}

func TestFilterSemanticPoliciesRespectsExplicitPluginActivation(t *testing.T) {
	contributions := []plugins.SemanticPolicyContribution{{PluginID: "php"}, {PluginID: "wordpress"}}
	got := filterSemanticPolicies(contributions, []string{"wordpress"})
	if len(got) != 1 || got[0].PluginID != "wordpress" {
		t.Fatalf("filtered contributions = %#v", got)
	}
}

func TestPluginSemanticPolicies_NoPluginsIsEmpty(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	ch := core.NewAppChannels()

	contributions, cleanup := PluginSemanticPolicies(context.Background(), cfg, &ch)
	if cleanup == nil {
		t.Fatal("cleanup must be non-nil and safe to call")
	}
	defer cleanup()
	if len(contributions) != 0 {
		t.Fatalf("expected no semantic policies from a plugin-less data dir, got %#v", contributions)
	}
}
