package iir

import (
	"strings"
	"testing"
)

func testableIntent() *FunctionIntent {
	return &FunctionIntent{
		Kind:         KindFunctionIntent,
		Name:         "f",
		Language:     "typescript",
		Behavior:     []BehaviorClause{{When: "x is negative", Then: "return neg"}},
		FailureModes: []string{"bad_input"},
		SideEffects:  []string{"analytics.track"},
	}
}

func TestGenerateTests_CasePerExpectation(t *testing.T) {
	art, err := GenerateTests(testableIntent())
	if err != nil {
		t.Fatalf("GenerateTests: %v", err)
	}
	if len(art.Coverage) != 3 {
		t.Fatalf("coverage = %d entries, want 3 (behavior + failure + side effect)", len(art.Coverage))
	}
	for _, c := range art.Coverage {
		if !c.Covered {
			t.Errorf("expectation %s should be covered", c.NodeID)
		}
	}

	wantNames := []string{
		`it("when x is negative then return neg"`,
		`it("fails with bad_input"`,
		`it("performs side effect analytics.track"`,
	}
	for _, w := range wantNames {
		if !strings.Contains(art.Source, w) {
			t.Errorf("source missing %s:\n%s", w, art.Source)
		}
	}
}

func TestGenerateTests_TraceabilityIds(t *testing.T) {
	art, _ := GenerateTests(testableIntent())
	wantIDs := []string{
		"// iir: f.behavior[0]",
		"// iir: f.failureMode.bad_input",
		"// iir: f.sideEffect.analytics.track",
	}
	for _, id := range wantIDs {
		if !strings.Contains(art.Source, id) {
			t.Errorf("source missing traceability %q", id)
		}
	}
	// The coverage report carries the same node ids.
	if art.Coverage[0].NodeID != "f.behavior[0]" {
		t.Errorf("coverage node id = %q", art.Coverage[0].NodeID)
	}
}

func TestGenerateTests_UnsupportedBehaviorReportedNotInvented(t *testing.T) {
	intent := &FunctionIntent{
		Kind:     KindFunctionIntent,
		Name:     "f",
		Language: "typescript",
		Behavior: []BehaviorClause{{When: "", Then: ""}}, // no description to test
	}
	art, err := GenerateTests(intent)
	if err != nil {
		t.Fatalf("GenerateTests: %v", err)
	}
	if len(art.Coverage) != 1 || art.Coverage[0].Covered {
		t.Fatalf("expected one uncovered expectation, got %+v", art.Coverage)
	}
	if art.Coverage[0].Reason == "" {
		t.Error("unsupported expectation must carry a reason")
	}
	if strings.Contains(art.Source, "it(") {
		t.Errorf("must not invent a test for an unsupported expectation:\n%s", art.Source)
	}
	if !strings.Contains(art.Source, "// unsupported") {
		t.Errorf("source should note the unsupported expectation:\n%s", art.Source)
	}
	// With nothing covered, a pending placeholder keeps the suite non-empty.
	if !strings.Contains(art.Source, "it.todo(") {
		t.Errorf("expected an it.todo placeholder for an all-unsupported suite:\n%s", art.Source)
	}
}

func TestGenerateTests_NoExpectationsEmitsTodo(t *testing.T) {
	// A function with no behavior, failure modes, or side effects has nothing to
	// test — emit a valid, non-empty suite with a pending placeholder.
	intent := &FunctionIntent{Kind: KindFunctionIntent, Name: "noop", Language: "typescript"}
	art, err := GenerateTests(intent)
	if err != nil {
		t.Fatalf("GenerateTests: %v", err)
	}
	if len(art.Coverage) != 0 {
		t.Errorf("expected empty coverage, got %+v", art.Coverage)
	}
	if !strings.Contains(art.Source, "it.todo(") {
		t.Errorf("expected an it.todo placeholder:\n%s", art.Source)
	}
	if strings.Contains(art.Source, "it(") {
		t.Errorf("should not emit a real test:\n%s", art.Source)
	}
}

func TestGenerateTests_RejectsInvalidName(t *testing.T) {
	// A name that isn't a valid identifier would produce broken import/expect
	// code, so generation must refuse it rather than emit unsafe source.
	for _, bad := range []string{"has space", "a;b", "1abc", "a\nb", ""} {
		intent := &FunctionIntent{Kind: KindFunctionIntent, Name: bad, Language: "typescript"}
		if _, err := GenerateTests(intent); err == nil {
			t.Errorf("expected error for invalid name %q", bad)
		}
	}
}

func TestGenerateTests_Deterministic(t *testing.T) {
	first, err := GenerateTests(testableIntent())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, _ := GenerateTests(testableIntent())
		if again.Source != first.Source {
			t.Fatalf("test generation not deterministic:\n%s\n---\n%s", first.Source, again.Source)
		}
	}
}

func TestGenerateTests_RejectsNonFunctionIntent(t *testing.T) {
	if _, err := GenerateTests(nil); err == nil {
		t.Error("expected error for nil intent")
	}
	if _, err := GenerateTests(&FunctionIntent{Kind: "Other", Name: "x"}); err == nil {
		t.Error("expected error for unsupported kind")
	}
}

func TestBuiltinTestEmitter_ResolvesAndEmits(t *testing.T) {
	intent := testableIntent()
	if !BuiltinTestEmitter().Supports(intent) {
		t.Error("expected support for a TypeScript FunctionIntent")
	}
	te, ok := DefaultRegistry().TestEmitterFor(intent)
	if !ok {
		t.Fatal("registry should resolve the built-in test emitter")
	}
	art, err := te.EmitTests(intent)
	if err != nil {
		t.Fatalf("EmitTests: %v", err)
	}
	if !strings.Contains(art.Source, "describe(\"f\"") {
		t.Errorf("emitted tests missing describe block:\n%s", art.Source)
	}
}
