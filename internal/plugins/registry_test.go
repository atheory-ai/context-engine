package plugins

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

type testLanguage struct{}

func (testLanguage) Extensions() []string { return []string{".php"} }
func (testLanguage) GrammarPath() string  { return "" }
func (testLanguage) Match(string) bool    { return false }
func (testLanguage) HasCustomMatch() bool { return false }
func (testLanguage) Extract(string, []byte, []byte) (core.ExtractionResult, error) {
	return core.ExtractionResult{}, nil
}
func (testLanguage) Concepts() []core.ConceptSeed { return nil }

type contractPlugin struct {
	basePlugin
	contract core.PluginIndexContract
}

func (p contractPlugin) Language() core.LanguageHandler          { return testLanguage{} }
func (p contractPlugin) IndexContract() core.PluginIndexContract { return p.contract }

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

type semanticContributorPlugin struct {
	basePlugin
	policies []byte
}

func TestIndexPlanForFile_UsesCapabilitiesNotLoadOrder(t *testing.T) {
	r := NewRegistry()
	wp := contractPlugin{basePlugin: basePlugin{"wp"}, contract: core.PluginIndexContract{Requires: []string{"cst:php", "facts:php-structure"}, Enriches: []string{"php"}}}
	php := contractPlugin{basePlugin: basePlugin{"php"}, contract: core.PluginIndexContract{Provides: []string{"language:php", "cst:php", "facts:php-structure"}}}
	r.plugins = map[core.PluginID]core.Plugin{"wp": wp, "php": php}
	r.loadOrder = []core.PluginID{"wp", "php"} // deliberately reversed
	plan, err := r.IndexPlanForFile("theme.php")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].ID() != "php" || plan[1].ID() != "wp" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestIndexPlanForFile_RejectsMissingProvider(t *testing.T) {
	r := NewRegistry()
	r.plugins = map[core.PluginID]core.Plugin{"wp": contractPlugin{basePlugin: basePlugin{"wp"}, contract: core.PluginIndexContract{Requires: []string{"cst:php"}}}}
	r.loadOrder = []core.PluginID{"wp"}
	if _, err := r.IndexPlanForFile("theme.php"); err == nil {
		t.Fatal("expected missing provider error")
	}
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

func (c semanticContributorPlugin) SemanticPoliciesJSON() []byte { return c.policies }

func TestSemanticPolicyContributionsPreservePluginOriginAndOrder(t *testing.T) {
	r := NewRegistry()
	r.plugins = map[core.PluginID]core.Plugin{
		"a": semanticContributorPlugin{basePlugin: basePlugin{"a"}, policies: []byte(`{"schemaVersion":"v1","policies":[{"id":"a.rule"}]}`)},
		"b": basePlugin{"b"},
		"c": semanticContributorPlugin{basePlugin: basePlugin{"c"}, policies: nil},
		"d": semanticContributorPlugin{basePlugin: basePlugin{"d"}, policies: []byte(`{"schemaVersion":"v1","policies":[{"id":"d.rule"}]}`)},
	}
	r.loadOrder = []core.PluginID{"a", "b", "c", "d"}

	got := r.SemanticPolicyContributions()
	if len(got) != 2 {
		t.Fatalf("want 2 contributions, got %#v", got)
	}
	if got[0].PluginID != "a" || got[1].PluginID != "d" {
		t.Fatalf("plugin order = %#v", got)
	}
	if string(got[0].Raw) == "" || got[0].Version != "0" {
		t.Fatalf("contribution origin = %#v", got[0])
	}
}
