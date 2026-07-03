package iir

import "testing"

func resultByID(results []RuleResult, id string) *RuleResult {
	for i := range results {
		if results[i].ID == id {
			return &results[i]
		}
	}
	return nil
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
