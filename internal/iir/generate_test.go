package iir

import (
	"context"
	"strings"
	"testing"
)

// roundTrip generates source from intent, then verifies the generated source
// back against the same intent — the core Slice 6 loop.
func roundTrip(t *testing.T, intent *FunctionIntent) (string, *Report) {
	t.Helper()
	src, err := GenerateFunction(intent)
	if err != nil {
		t.Fatalf("GenerateFunction: %v", err)
	}
	report, err := VerifySource(context.Background(), intent, []byte(src), DefaultRulePack())
	if err != nil {
		t.Fatalf("VerifySource on generated source: %v\n--- source ---\n%s", err, src)
	}
	return src, report
}

func errorMismatches(report *Report) []Mismatch {
	var out []Mismatch
	for _, m := range report.Mismatches {
		if m.Severity == SeverityError {
			out = append(out, m)
		}
	}
	return out
}

func TestGenerate_ResultStrategyRoundTrips(t *testing.T) {
	intent := mustLoad(t, `
kind: FunctionIntent
name: validateDonationAmount
language: typescript
inputs:
  - name: amount
    type: Money
  - name: campaign
    type: Campaign
returns:
  type: ValidationResult<Money>
behavior:
  - when: amount is below campaign.minimumDonation
    then: return validation error amount_below_minimum
sideEffects: []
failureModes:
  - amount_below_minimum
`)
	src, report := roundTrip(t, intent)
	t.Logf("generated:\n%s", src)

	if errs := errorMismatches(report); len(errs) != 0 {
		t.Errorf("round-trip produced error mismatches: %+v", errs)
	}
	if report.Status != StatusPassed {
		t.Errorf("round-trip status = %s, want passed", report.Status)
	}
	if !strings.HasPrefix(src, "export function validateDonationAmount(") {
		t.Errorf("unexpected signature:\n%s", src)
	}
}

func TestGenerate_ThrowStrategyRoundTripsFailureModes(t *testing.T) {
	// A non-Result return with failure modes uses throws, which the extractor
	// observes — so failure modes round-trip cleanly here.
	intent := mustLoad(t, `
kind: FunctionIntent
name: parsePort
language: typescript
inputs:
  - name: raw
    type: string
returns:
  type: number
sideEffects: []
failureModes:
  - invalid_port
`)
	_, report := roundTrip(t, intent)
	if errs := errorMismatches(report); len(errs) != 0 {
		t.Errorf("round-trip error mismatches: %+v", errs)
	}
	// Thrown failure mode should be observed, so no changed_failure_mode.
	if findMismatch(report.Mismatches, MismatchChangedFailureMode) != nil {
		t.Errorf("expected failure mode to round-trip via throw: %+v", report.Mismatches)
	}
}

func TestGenerate_DeclaredSideEffectsRoundTrip(t *testing.T) {
	intent := mustLoad(t, `
kind: FunctionIntent
name: recordDonation
language: typescript
inputs:
  - name: amount
    type: number
returns:
  type: void
sideEffects:
  - analytics.track
`)
	_, report := roundTrip(t, intent)
	if m := findMismatch(report.Mismatches, MismatchUndeclaredEffect); m != nil {
		t.Errorf("declared side effect should not be undeclared: %+v", m)
	}
	if m := findMismatch(report.Mismatches, MismatchUndetectedEffect); m != nil {
		t.Errorf("declared side effect should be detected in generated source: %+v", m)
	}
}

func TestGenerate_BehaviorCountRoundTrips(t *testing.T) {
	intent := mustLoad(t, `
kind: FunctionIntent
name: classify
language: typescript
inputs:
  - name: n
    type: number
returns:
  type: string
sideEffects: []
behavior:
  - when: n is negative
    then: return neg
  - when: n is zero
    then: return zero
`)
	_, report := roundTrip(t, intent)
	if findMismatch(report.Mismatches, MismatchMissingBehavior) != nil ||
		findMismatch(report.Mismatches, MismatchExtraBehavior) != nil {
		t.Errorf("behavior count should round-trip: %+v", report.Mismatches)
	}
}

func TestGenerate_RespectsRulePacks(t *testing.T) {
	// A rule-compliant intent (public, explicit return, declared side effects)
	// must generate rule-compliant code.
	intent := mustLoad(t, `
kind: FunctionIntent
name: total
language: typescript
inputs:
  - name: xs
    type: number[]
returns:
  type: number
sideEffects: []
`)
	_, report := roundTrip(t, intent)
	for _, r := range report.RuleResults {
		if r.Status == RuleFailed {
			t.Errorf("generated code violates rule %s: %s", r.ID, r.Message)
		}
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	intent := mustLoad(t, `
kind: FunctionIntent
name: f
language: typescript
inputs:
  - name: a
    type: number
returns:
  type: void
sideEffects:
  - analytics.track
  - db.save
`)
	first, err := GenerateFunction(intent)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := GenerateFunction(intent)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("generation not deterministic:\n%s\n---\n%s", first, again)
		}
	}
}

func TestGenerate_RejectsNonFunctionIntent(t *testing.T) {
	if _, err := GenerateFunction(nil); err == nil {
		t.Error("expected error for nil intent")
	}
	if _, err := GenerateFunction(&FunctionIntent{Kind: "Other", Name: "x"}); err == nil {
		t.Error("expected error for unsupported kind")
	}
}

func TestBuiltinEmitter_SupportsAndEmits(t *testing.T) {
	em := BuiltinEmitter()
	if !em.Supports(&FunctionIntent{Kind: KindFunctionIntent, Language: "typescript", Name: "f"}) {
		t.Error("expected support for a TypeScript FunctionIntent")
	}
	reg := DefaultRegistry()
	if _, ok := reg.EmitterFor(&FunctionIntent{Kind: KindFunctionIntent, Name: "f"}); !ok {
		t.Error("registry should resolve the built-in emitter")
	}
}
