package iir

import (
	"context"
	"testing"
)

func TestBuiltinExtractor_SupportsTypeScript(t *testing.T) {
	ext := BuiltinExtractor()
	cases := []struct {
		in   ExtractionInput
		want bool
	}{
		{ExtractionInput{Language: "typescript"}, true},
		{ExtractionInput{Language: "", Path: "foo.ts"}, true},
		{ExtractionInput{Language: "", Path: "foo.tsx"}, true},
		{ExtractionInput{Language: "go"}, false},
		{ExtractionInput{Language: "", Path: "foo.go"}, false},
	}
	for _, c := range cases {
		if got := ext.Supports(c.in); got != c.want {
			t.Errorf("Supports(%+v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBuiltinExtractor_Extract(t *testing.T) {
	res, err := BuiltinExtractor().Extract(context.Background(), ExtractionInput{
		Language: "typescript",
		Source:   []byte(`export function f(x: number): number { return x; }`),
		Target:   "f",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.Function == nil || res.Function.Name != "f" {
		t.Errorf("unexpected result: %+v", res.Function)
	}
}

func TestBuiltinComparator_Supports(t *testing.T) {
	cmp := BuiltinComparator()
	if !cmp.Supports(baseIntent(), baseIntent()) {
		t.Error("expected support for a FunctionIntent pair")
	}
	if cmp.Supports(nil, baseIntent()) {
		t.Error("nil intended should be unsupported")
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
	p := BuiltinPlugin()
	if len(p.Extractors) == 0 || len(p.Comparators) == 0 {
		t.Fatalf("built-in plugin missing capabilities: %+v", p)
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

func TestRegistry_ResolvesBuiltins(t *testing.T) {
	reg := DefaultRegistry()
	if _, ok := reg.ExtractorFor(ExtractionInput{Language: "typescript"}); !ok {
		t.Error("expected a TypeScript extractor")
	}
	if _, ok := reg.ExtractorFor(ExtractionInput{Language: "cobol"}); ok {
		t.Error("did not expect an extractor for cobol")
	}
	if _, ok := reg.ComparatorFor(baseIntent(), baseIntent()); !ok {
		t.Error("expected a FunctionIntent comparator")
	}
}

// fakeExtractor lets a test register a competing extractor.
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
