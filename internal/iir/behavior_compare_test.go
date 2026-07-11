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

// When either side lacks a WhenExpr, comparison falls back to the count-based
// behavior exactly — no regression, even if the raw strings differ.
func TestCompare_BehaviorFallsBackWhenUnstructured(t *testing.T) {
	intended := behaviorIntent("a < 1", bin("<", path("a"), lit("1")))
	extracted := behaviorIntent("something opaque", nil) // no structured form
	_, mismatches := Compare(intended, extracted)
	if findMismatch(mismatches, MismatchBehaviorContent) != nil {
		t.Errorf("unstructured side must not produce a content mismatch: %+v", mismatches)
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
