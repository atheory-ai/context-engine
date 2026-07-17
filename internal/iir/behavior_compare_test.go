package iir

import (
	"encoding/json"
	"strings"
	"testing"
)

func behaviorIntent(when string, expr *Expr) *FunctionIntent {
	f := baseIntent()
	f.Behavior = []BehaviorClause{{When: when, Then: "return 0", WhenExpr: expr}}
	return f
}

func TestCompare_BehaviorContentMismatch(t *testing.T) {
	intended := behaviorIntent("a < 1", bin("<", path("a"), lit("1")))
	extracted := behaviorIntent("a > 1", bin(">", path("a"), lit("1")))
	_, mismatches := Compare(intended, extracted)
	m := findMismatch(mismatches, MismatchBehaviorContent)
	if m == nil {
		t.Fatalf("expected a mismatched_behavior finding, got %+v", mismatches)
	}
	if m.Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", m.Severity)
	}
	// The count-only path must NOT also report a spurious match.
	if findMismatch(mismatches, MismatchMissingBehavior) != nil {
		t.Error("content mismatch should not coexist with a count mismatch")
	}
}

func TestCompare_BehaviorContentMatches(t *testing.T) {
	intended := behaviorIntent("a < 1", bin("<", path("a"), lit("1")))
	extracted := behaviorIntent("a<1", bin("<", path("a"), lit("1"))) // whitespace differs, structure same
	matches, mismatches := Compare(intended, extracted)
	if findMismatch(mismatches, MismatchBehaviorContent) != nil {
		t.Errorf("structurally-equal conditions should not mismatch: %+v", mismatches)
	}
	found := false
	for _, mt := range matches {
		if mt.Path == "FunctionIntent.behavior" {
			found = true
		}
	}
	if !found {
		t.Error("expected a behavior match")
	}
}

// A prose-only condition has no semantic form to compare. Equal branch counts
// are not enough to establish behavior equivalence.
func TestCompare_BehaviorIsInconclusiveWhenUnstructured(t *testing.T) {
	intended := behaviorIntent("a < 1", bin("<", path("a"), lit("1")))
	extracted := behaviorIntent("something opaque", nil) // no structured form
	_, mismatches := Compare(intended, extracted)
	if findMismatch(mismatches, MismatchUnsupported) == nil {
		t.Errorf("unstructured behavior must be unsupported: %+v", mismatches)
	}
}

func TestVerify_IsInconclusiveForProseOnlyBehavior(t *testing.T) {
	intended := baseIntent()
	intended.Behavior = []BehaviorClause{{When: "entityKey is empty", Then: "throw invalid_entity_key"}}
	extracted := baseIntent()
	extracted.Behavior = []BehaviorClause{{When: "false", Then: "throw invalid_entity_key", WhenExpr: lit("false")}}

	if report := Verify(intended, extracted, DefaultRulePack()); report.Status != StatusInconclusive {
		t.Fatalf("status = %s, want %s; mismatches: %+v", report.Status, StatusInconclusive, report.Mismatches)
	}
}

func TestBehaviorClause_JSONOmitsWhenExprWhenAbsent(t *testing.T) {
	b, err := json.Marshal(BehaviorClause{When: "x < 1", Then: "return 0"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "whenExpr") {
		t.Errorf("absent WhenExpr must be omitted, got %s", b)
	}
}

func TestBehaviorClause_JSONRoundTrip(t *testing.T) {
	orig := BehaviorClause{When: "a < 1", Then: "return 0", WhenExpr: bin("<", path("a"), lit("1"))}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "whenExpr") {
		t.Errorf("present WhenExpr must serialize, got %s", b)
	}
	var back BehaviorClause
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.WhenExpr.Equal(orig.WhenExpr) {
		t.Errorf("round-trip changed WhenExpr: %+v", back.WhenExpr)
	}
}
