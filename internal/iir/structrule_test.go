package iir

import "testing"

// The shipped example pack must stay loadable under strict parsing, including
// the structural rule it now documents.
func TestExampleRulePack_Loads(t *testing.T) {
	pack, err := LoadRulePackFile("../../examples/iir.rules.yaml")
	if err != nil {
		t.Fatalf("example rule pack failed to load: %v", err)
	}
	if !hasRuleID(pack.Rules, "forbid-null-equality") {
		t.Error("example pack should document the forbid-null-equality structural rule")
	}
}

// forbidNullEquality is the concrete opinion this slice proves: a structural
// rule that forbids comparing against a null literal (a real footgun, and a
// deliberately opinionated rule — hence contributed, not a built-in default).
func forbidNullEquality() RulePack {
	return RulePack{Rules: []Rule{{
		ID:       "forbid-null-equality",
		Target:   KindFunctionIntent,
		Severity: SeverityError,
		Require: RuleRequire{ForbidConditionShape: &ExprPattern{
			Ops:            []string{"==", "!=", "===", "!=="},
			OperandLiteral: ptrTo("null"),
		}},
	}}}
}

func TestForbidConditionShape_NullEqualityFails(t *testing.T) {
	intent := behaviorIntent("x === null", bin("===", path("x"), lit("null")))
	r := resultByID(EvaluateRules(forbidNullEquality(), intent), "forbid-null-equality")
	if r == nil || r.Status != RuleFailed {
		t.Fatalf("expected forbid-null-equality to fail, got %+v", r)
	}
	if r.Repair == "" {
		t.Error("failed structural rule must include repair guidance")
	}
}

func TestForbidConditionShape_PassesWhenNoMatch(t *testing.T) {
	intent := behaviorIntent("x < 1", bin("<", path("x"), lit("1")))
	r := resultByID(EvaluateRules(forbidNullEquality(), intent), "forbid-null-equality")
	if r == nil || r.Status != RulePassed {
		t.Fatalf("expected pass for a non-matching condition, got %+v", r)
	}
}

func TestForbidConditionShape_MatchesNestedSubtree(t *testing.T) {
	// The forbidden shape is buried under a logical connective.
	expr := bin("&&", bin(">", path("a"), lit("0")), bin("!=", path("x"), lit("null")))
	intent := behaviorIntent("a > 0 && x != null", expr)
	r := resultByID(EvaluateRules(forbidNullEquality(), intent), "forbid-null-equality")
	if r == nil || r.Status != RuleFailed {
		t.Fatalf("expected nested match to fail, got %+v", r)
	}
}

// A condition outside the normalized grammar has no WhenExpr, so a structural
// rule must not match it — no false positives from unstructured conditions.
func TestForbidConditionShape_SkipsUnstructured(t *testing.T) {
	intent := behaviorIntent("isNullish(x)", nil)
	r := resultByID(EvaluateRules(forbidNullEquality(), intent), "forbid-null-equality")
	if r == nil || r.Status != RulePassed {
		t.Fatalf("unstructured condition must not trip a structural rule, got %+v", r)
	}
}

func TestForbidConditionShape_OperandLiteralOnly(t *testing.T) {
	// No Ops — any operator whose operand is the literal `true`.
	pack := RulePack{Rules: []Rule{{
		ID: "forbid-eq-true", Target: KindFunctionIntent, Severity: SeverityWarning,
		Require: RuleRequire{ForbidConditionShape: &ExprPattern{OperandLiteral: ptrTo("true")}},
	}}}
	intent := behaviorIntent("flag === true", bin("===", path("flag"), lit("true")))
	r := resultByID(EvaluateRules(pack, intent), "forbid-eq-true")
	if r == nil || r.Status != RuleWarned {
		t.Fatalf("expected warn for `=== true`, got %+v", r)
	}
}

func TestForbidConditionShape_OpsOnly(t *testing.T) {
	// No OperandLiteral — any negation node anywhere in the condition.
	pack := RulePack{Rules: []Rule{{
		ID: "forbid-negation", Target: KindFunctionIntent, Severity: SeverityError,
		Require: RuleRequire{ForbidConditionShape: &ExprPattern{Ops: []string{"!"}}},
	}}}
	intent := behaviorIntent("!ready", &Expr{Op: "!", Args: []*Expr{path("ready")}})
	r := resultByID(EvaluateRules(pack, intent), "forbid-negation")
	if r == nil || r.Status != RuleFailed {
		t.Fatalf("expected fail for negation, got %+v", r)
	}
}

// The whole point of the slice: a plugin/project can express this in a rule
// pack (strict-parsed YAML), not just in Go.
func TestForbidConditionShape_LoadsFromYAML(t *testing.T) {
	pack, err := LoadRulePack([]byte(`rules:
  - id: forbid-null-equality
    target: FunctionIntent
    severity: error
    require:
      forbidConditionShape:
        ops: ["==", "!=", "===", "!=="]
        operandLiteral: "null"
`))
	if err != nil {
		t.Fatalf("load rule pack: %v", err)
	}
	intent := behaviorIntent("x == null", bin("==", path("x"), lit("null")))
	r := resultByID(EvaluateRules(pack, intent), "forbid-null-equality")
	if r == nil || r.Status != RuleFailed {
		t.Fatalf("expected YAML-loaded structural rule to fail, got %+v", r)
	}
}

// An all-empty pattern would match every condition — a footgun the loader must
// reject rather than silently forbid all branches.
func TestForbidConditionShape_RejectsEmptyPattern(t *testing.T) {
	_, err := LoadRulePack([]byte(`rules:
  - id: bad
    target: FunctionIntent
    severity: error
    require:
      forbidConditionShape: {}
`))
	if err == nil {
		t.Fatal("expected an error for an empty forbidConditionShape pattern")
	}
}
