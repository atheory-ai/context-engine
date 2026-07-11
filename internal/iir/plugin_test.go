package iir

import (
	"context"
	"testing"
)

func TestBuiltinComparator_Supports(t *testing.T) {
	cmp := BuiltinComparator()
	if !cmp.Supports(baseIntent(), baseIntent()) {
		t.Error("expected support for a FunctionIntent pair")
	}
	if cmp.Supports(nil, baseIntent()) {
		t.Error("nil intended should be unsupported")
	}
	if cmp.Supports(baseIntent(), &FunctionIntent{Kind: ""}) {
		t.Error("extracted node without FunctionIntent kind should be unsupported")
	}
}

func TestBuiltinComparator_CompareMatchesFreeFunction(t *testing.T) {
	// The interface must produce the same result as the underlying Compare.
	extracted := baseIntent()
	extracted.Name = "other"
	viaInterface := BuiltinComparator().Compare(baseIntent(), extracted)
	if findMismatch(viaInterface.Mismatches, MismatchName) == nil {
		t.Errorf("comparator interface should surface name mismatch: %+v", viaInterface.Mismatches)
	}
}

func TestBuiltinPlugin_CapabilitiesAndRuleProvenance(t *testing.T) {
	// The built-in plugin no longer ships an extractor (lift is plugin-owned); it
	// still provides the comparator, emitters, and the default rule pack.
	p := BuiltinPlugin()
	if len(p.Comparators) == 0 {
		t.Fatalf("built-in plugin missing comparator: %+v", p)
	}
	if len(p.RulePacks) != 1 {
		t.Fatalf("expected one built-in rule pack, got %d", len(p.RulePacks))
	}
	if p.RulePacks[0].PluginID != builtinPluginID {
		t.Errorf("rule pack plugin id = %q, want %q", p.RulePacks[0].PluginID, builtinPluginID)
	}
	if len(p.RulePacks[0].Pack.Rules) == 0 {
		t.Error("built-in rule pack should carry the default rules")
	}
}

func TestRegistry_ResolvesComparator(t *testing.T) {
	reg := DefaultRegistry()
	if _, ok := reg.ComparatorFor(baseIntent(), baseIntent()); !ok {
		t.Error("expected a FunctionIntent comparator")
	}
}

// fakeExtractor lets a test register a competing extractor, exercising the
// registry's precedence rules independently of any built-in extractor.
type fakeExtractor struct{}

func (fakeExtractor) ID() string                       { return "fake.typescript" }
func (fakeExtractor) Supports(in ExtractionInput) bool { return in.Language == "typescript" }
func (fakeExtractor) Extract(context.Context, ExtractionInput) (ExtractionResult, error) {
	return ExtractionResult{Function: &FunctionIntent{Name: "fake"}}, nil
}

func TestRegistry_LaterRegistrationTakesPrecedence(t *testing.T) {
	reg := DefaultRegistry()
	reg.Register(Plugin{ID: "override", Extractors: []Extractor{fakeExtractor{}}})

	ext, ok := reg.ExtractorFor(ExtractionInput{Language: "typescript"})
	if !ok {
		t.Fatal("expected an extractor")
	}
	if ext.ID() != "fake.typescript" {
		t.Errorf("expected the later-registered extractor to win, got %q", ext.ID())
	}
}

func TestRegistry_RulePacksCarryProvenance(t *testing.T) {
	reg := DefaultRegistry()
	packs := reg.RulePacks()
	if len(packs) != 1 || packs[0].PluginID != builtinPluginID {
		t.Errorf("unexpected rule packs: %+v", packs)
	}
}
