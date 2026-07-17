package iir

import (
	"encoding/json"
	"testing"
)

// The source-extraction verify tests live in internal/runner (they need the
// plugin lift); this keeps the pure report-shape check.
func TestVerify_ReportShapeIsStable(t *testing.T) {
	intent := mustLoad(t, validIntentYAML)
	extracted := &FunctionIntent{
		Kind: KindFunctionIntent, Name: "validateDonationAmount", Language: "typescript",
		Visibility: VisibilityPublic,
		Inputs:     []Param{{Name: "amount", Type: "Money"}},
		Returns:    Return{Type: "ValidationResult<Money>", Explicit: true},
		Behavior:   []BehaviorClause{}, SideEffects: []SideEffect{}, FailureModes: []FailureMode{}, Constraints: []string{},
	}
	report := Verify(intent, extracted, DefaultRulePack())

	// Every top-level collection must serialize as an array, never null, so
	// agents and tests can rely on the shape.
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"matches", "mismatches", "ruleResults", "repairTargets"} {
		v, ok := m[field]
		if !ok {
			t.Errorf("report missing field %q", field)
			continue
		}
		if string(v) == "null" {
			t.Errorf("field %q serialized as null, want array", field)
		}
	}
	for _, field := range []string{"status", "intended", "extracted"} {
		if _, ok := m[field]; !ok {
			t.Errorf("report missing field %q", field)
		}
	}
}

func TestVerify_FailsForDeclaredButUndetectedEffect(t *testing.T) {
	intended := mustLoad(t, validIntentYAML)
	intended.SideEffects = stringEffects("cache.invalidate")
	extracted := &FunctionIntent{
		Kind: KindFunctionIntent, Name: intended.Name, Language: intended.Language,
		Visibility:   VisibilityPublic,
		Inputs:       intended.Inputs,
		Returns:      intended.Returns,
		Behavior:     intended.Behavior,
		SideEffects:  []SideEffect{},
		FailureModes: intended.FailureModes,
		Constraints:  []string{},
	}

	report := Verify(intended, extracted, DefaultRulePack())
	if report.Status != StatusFailed {
		t.Fatalf("status = %s, want %s; mismatches: %+v", report.Status, StatusFailed, report.Mismatches)
	}
}

func mustLoad(t *testing.T, doc string) *FunctionIntent {
	t.Helper()
	intent, err := LoadIntent([]byte(doc))
	if err != nil {
		t.Fatalf("LoadIntent: %v", err)
	}
	return intent
}
