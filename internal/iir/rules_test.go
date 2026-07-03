package iir

import (
	"os"
	"path/filepath"
	"testing"
)

func resultByID(results []RuleResult, id string) *RuleResult {
	for i := range results {
		if results[i].ID == id {
			return &results[i]
		}
	}
	return nil
}

func hasRuleID(rules []Rule, id string) bool {
	for _, r := range rules {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestDefaultRules_PassForCompliantFunction(t *testing.T) {
	intent := &FunctionIntent{
		Kind:        KindFunctionIntent,
		Name:        "f",
		Visibility:  VisibilityPublic,
		Returns:     Return{Type: "number", Explicit: true},
		SideEffects: []string{}, // declares "no side effects"
	}
	results := EvaluateRules(DefaultRulePack(), intent)
	for _, r := range results {
		if r.Status == RuleFailed {
			t.Errorf("rule %s unexpectedly failed: %s", r.ID, r.Message)
		}
	}
}

func TestDefaultRules_ExplicitReturnTypeFails(t *testing.T) {
	intent := &FunctionIntent{
		Kind:        KindFunctionIntent,
		Name:        "f",
		Visibility:  VisibilityPublic,
		Returns:     Return{Explicit: false},
		SideEffects: []string{},
	}
	results := EvaluateRules(DefaultRulePack(), intent)
	r := resultByID(results, "function-explicit-return-type")
	if r == nil || r.Status != RuleFailed {
		t.Fatalf("expected function-explicit-return-type to fail, got %+v", r)
	}
	if r.Repair == "" {
		t.Error("failed rule must include repair guidance")
	}
}

func TestRules_VisibilityWhenSkipsPrivate(t *testing.T) {
	intent := &FunctionIntent{
		Kind:        KindFunctionIntent,
		Name:        "f",
		Visibility:  VisibilityPrivate, // rule targets public only
		Returns:     Return{Explicit: false},
		SideEffects: []string{},
	}
	results := EvaluateRules(DefaultRulePack(), intent)
	r := resultByID(results, "function-explicit-return-type")
	if r == nil || r.Status != RuleSkipped {
		t.Fatalf("expected rule skipped for private function, got %+v", r)
	}
}

func TestRules_SideEffectsDeclaredFailsWhenNil(t *testing.T) {
	intent := &FunctionIntent{
		Kind:        KindFunctionIntent,
		Name:        "f",
		Visibility:  VisibilityPublic,
		Returns:     Return{Type: "void", Explicit: true},
		SideEffects: nil, // undeclared
	}
	results := EvaluateRules(DefaultRulePack(), intent)
	r := resultByID(results, "declare-side-effects")
	if r == nil || r.Status != RuleFailed {
		t.Fatalf("expected declare-side-effects to fail when nil, got %+v", r)
	}
}

func TestRules_ResultTypeStrategy(t *testing.T) {
	trueVal := true
	strategy := "ResultType"
	pack := RulePack{Rules: []Rule{{
		ID:       "expected-failures-use-result",
		Target:   KindFunctionIntent,
		Severity: SeverityWarning,
		When:     RuleWhen{HasFailureModes: &trueVal},
		Require:  RuleRequire{FailureStrategy: &strategy},
	}}}

	thrown := &FunctionIntent{
		Kind: KindFunctionIntent, Name: "f", Visibility: VisibilityPublic,
		Returns: Return{Type: "void", Explicit: true}, FailureModes: []string{"bad"},
	}
	results := EvaluateRules(pack, thrown)
	if r := resultByID(results, "expected-failures-use-result"); r == nil || r.Status != RuleWarned {
		t.Errorf("expected warned for non-result return, got %+v", r)
	}

	result := &FunctionIntent{
		Kind: KindFunctionIntent, Name: "f", Visibility: VisibilityPublic,
		Returns: Return{Type: "ValidationResult<Money>", Explicit: true}, FailureModes: []string{"bad"},
	}
	results = EvaluateRules(pack, result)
	if r := resultByID(results, "expected-failures-use-result"); r == nil || r.Status != RulePassed {
		t.Errorf("expected pass for result-type return, got %+v", r)
	}
}

func TestRules_ExplicitReturnTypeFalseIsHonored(t *testing.T) {
	// require explicitReturnType:false must fail a function that HAS an
	// explicit return type — a false assertion is not a silent pass.
	falseVal := false
	pack := RulePack{Rules: []Rule{{
		ID: "no-explicit-return", Target: KindFunctionIntent, Severity: SeverityError,
		Require: RuleRequire{ExplicitReturnType: &falseVal},
	}}}
	intent := &FunctionIntent{
		Kind: KindFunctionIntent, Name: "f", Visibility: VisibilityPublic,
		Returns: Return{Type: "number", Explicit: true}, SideEffects: []string{},
	}
	if r := resultByID(EvaluateRules(pack, intent), "no-explicit-return"); r == nil || r.Status != RuleFailed {
		t.Errorf("expected false requirement to fail an explicit-return function, got %+v", r)
	}
}

func TestDefaultRulePack_IncludesFailureStrategyRule(t *testing.T) {
	if !hasRuleID(DefaultRulePack().Rules, "expected-failures-use-result") {
		t.Error("default pack should include expected-failures-use-result")
	}
}

func TestMergeRulePacks_OverridesByIDAndAppends(t *testing.T) {
	base := RulePack{Rules: []Rule{
		{ID: "a", Target: KindFunctionIntent, Severity: SeverityError},
		{ID: "b", Target: KindFunctionIntent, Severity: SeverityError},
	}}
	override := RulePack{Rules: []Rule{
		{ID: "b", Target: KindFunctionIntent, Severity: SeverityWarning}, // tune existing
		{ID: "c", Target: KindFunctionIntent, Severity: SeverityInfo},    // add new
	}}
	merged := MergeRulePacks(base, override)

	if len(merged.Rules) != 3 {
		t.Fatalf("merged rules = %d, want 3", len(merged.Rules))
	}
	// Base order preserved, override applied in place, new rule appended.
	if merged.Rules[0].ID != "a" || merged.Rules[1].ID != "b" || merged.Rules[2].ID != "c" {
		t.Errorf("unexpected order: %s,%s,%s", merged.Rules[0].ID, merged.Rules[1].ID, merged.Rules[2].ID)
	}
	if merged.Rules[1].Severity != SeverityWarning {
		t.Errorf("overridden rule b severity = %s, want warning", merged.Rules[1].Severity)
	}
	// Merge must not mutate the base pack.
	if base.Rules[1].Severity != SeverityError {
		t.Error("MergeRulePacks mutated the base pack")
	}
}

func TestDiscoverProjectRulePack_WalksUp(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "rules:\n  - id: x\n    target: FunctionIntent\n    severity: warning\n"
	if err := os.WriteFile(filepath.Join(root, "iir.rules.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	pack, path, found, err := DiscoverProjectRulePack(sub)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v, want found with no error", found, err)
	}
	if filepath.Base(path) != "iir.rules.yaml" {
		t.Errorf("path = %s, want .../iir.rules.yaml", path)
	}
	if len(pack.Rules) != 1 || pack.Rules[0].ID != "x" {
		t.Errorf("unexpected pack: %+v", pack)
	}
}

func TestDiscoverProjectRulePack_NotFound(t *testing.T) {
	_, _, found, err := DiscoverProjectRulePack(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected no project rule pack in an empty temp dir")
	}
}

func TestDiscoverProjectRulePack_InvalidSurfacesError(t *testing.T) {
	dir := t.TempDir()
	// An empty rules list is invalid; discovery must surface it, not ignore it.
	if err := os.WriteFile(filepath.Join(dir, "iir.rules.yaml"), []byte("rules: []"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, found, err := DiscoverProjectRulePack(dir)
	if !found {
		t.Error("expected found=true for an existing (if invalid) pack")
	}
	if err == nil {
		t.Error("expected an error surfacing the invalid pack")
	}
}

func TestLoadRulePack_Valid(t *testing.T) {
	doc := `
rules:
  - id: function-explicit-return-type
    target: FunctionIntent
    severity: error
    when:
      visibility: public
    require:
      explicitReturnType: true
`
	pack, err := LoadRulePack([]byte(doc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pack.Rules) != 1 || pack.Rules[0].ID != "function-explicit-return-type" {
		t.Errorf("unexpected pack: %+v", pack)
	}
	if pack.Rules[0].Require.ExplicitReturnType == nil || !*pack.Rules[0].Require.ExplicitReturnType {
		t.Error("explicitReturnType require not parsed")
	}
}

func TestLoadRulePack_Invalid(t *testing.T) {
	cases := map[string]string{
		"empty":                `rules: []`,
		"missing id":           "rules:\n  - target: FunctionIntent\n    severity: error",
		"unknown severity":     "rules:\n  - id: x\n    target: FunctionIntent\n    severity: fatal",
		"duplicate id":         "rules:\n  - id: x\n    target: FunctionIntent\n    severity: error\n  - id: x\n    target: FunctionIntent\n    severity: error",
		"unsupported target":   "rules:\n  - id: x\n    target: FuncIntent\n    severity: error",
		"unknown failureStrat": "rules:\n  - id: x\n    target: FunctionIntent\n    severity: warning\n    require:\n      failureStrategy: Throw",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadRulePack([]byte(doc)); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}
