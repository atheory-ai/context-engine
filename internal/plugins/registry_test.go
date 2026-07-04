package plugins

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

// basePlugin stubs core.Plugin; it does NOT contribute IIR rules.
type basePlugin struct{ id core.PluginID }

func (b basePlugin) ID() core.PluginID              { return b.id }
func (b basePlugin) Name() string                   { return string(b.id) }
func (b basePlugin) Version() string                { return "0" }
func (b basePlugin) Language() core.LanguageHandler { return nil }
func (b basePlugin) Roles() []core.RoleDefinition   { return nil }
func (b basePlugin) Analyzers() []core.Analyzer     { return nil }
func (b basePlugin) Tools() []core.Tool             { return nil }
func (b basePlugin) Close() error                   { return nil }

// contributorPlugin additionally implements the optional iir-rule interface.
type contributorPlugin struct {
	basePlugin
	rules []byte
}

func (c contributorPlugin) IIRRulePackJSON() []byte { return c.rules }

func TestIIRRulePackJSONs(t *testing.T) {
	r := NewRegistry()
	r.plugins = map[core.PluginID]core.Plugin{
		"a": contributorPlugin{basePlugin{"a"}, []byte(`{"rules":[{"id":"x"}]}`)},
		"b": basePlugin{"b"},                         // not a contributor
		"c": contributorPlugin{basePlugin{"c"}, nil}, // contributor but no rules
		"d": contributorPlugin{basePlugin{"d"}, []byte(`{"rules":[{"id":"y"}]}`)},
	}
	r.loadOrder = []core.PluginID{"a", "b", "c", "d"}

	got := r.IIRRulePackJSONs()
	if len(got) != 2 {
		t.Fatalf("want 2 packs (a, d), got %d", len(got))
	}
	// Order follows load order.
	if string(got[0]) != `{"rules":[{"id":"x"}]}` || string(got[1]) != `{"rules":[{"id":"y"}]}` {
		t.Errorf("unexpected packs or order: %q, %q", got[0], got[1])
	}
}
