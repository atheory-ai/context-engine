package iir

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// donationIntentYAML matches the two-input example source used in verify tests.
const donationIntentYAML = `
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
sideEffects: []
failureModes:
  - amount_below_minimum
`

func TestVerifySource_PassesForCleanImplementation(t *testing.T) {
	intent := mustLoad(t, donationIntentYAML)
	src := `
import { ok, err } from "./result";
export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> {
  if (amount.cents < campaign.minimumDonation.cents) {
    return err("amount_below_minimum");
  }
  return ok(amount);
}
`
	report, err := VerifySource(context.Background(), intent, []byte(src), DefaultRulePack())
	if err != nil {
		t.Fatalf("VerifySource: %v", err)
	}
	if report.Status != StatusPassed {
		t.Errorf("status = %s, want passed\nmismatches: %+v", report.Status, report.Mismatches)
	}
}

func TestVerifySource_FailsOnUndeclaredSideEffect(t *testing.T) {
	intent := mustLoad(t, donationIntentYAML)
	src := `
import { analytics } from "./analytics";
import { ok } from "./result";
export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> {
  analytics.track("validated");
  return ok(amount);
}
`
	report, err := VerifySource(context.Background(), intent, []byte(src), DefaultRulePack())
	if err != nil {
		t.Fatalf("VerifySource: %v", err)
	}
	if report.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", report.Status)
	}
	if findMismatch(report.Mismatches, MismatchUndeclaredEffect) == nil {
		t.Errorf("expected undeclared_side_effect mismatch, got %+v", report.Mismatches)
	}
	if len(report.RepairTargets) == 0 {
		t.Error("failed report must include repair targets")
	}
}

func TestVerify_ReportShapeIsStable(t *testing.T) {
	intent := mustLoad(t, validIntentYAML)
	extracted := &FunctionIntent{
		Kind: KindFunctionIntent, Name: "validateDonationAmount", Language: "typescript",
		Visibility: VisibilityPublic,
		Inputs:     []Param{{Name: "amount", Type: "Money"}},
		Returns:    Return{Type: "ValidationResult<Money>", Explicit: true},
		Behavior:   []BehaviorClause{}, SideEffects: []string{}, FailureModes: []string{}, Constraints: []string{},
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

// TestVerify_ExampleFiles guards the Slice 1 definition of done: the shipped
// example must produce a failing report (undeclared analytics.track), and the
// clean variant must pass.
func TestVerify_ExampleFiles(t *testing.T) {
	examples := filepath.Join("..", "..", "examples")
	intent, err := LoadIntentFile(filepath.Join(examples, "validateDonationAmount.iir.yaml"))
	if err != nil {
		t.Fatalf("load example intent: %v", err)
	}

	cases := []struct {
		file string
		want Status
	}{
		{"function-source-sample.ts", StatusFailed},
		{"function-source-clean.ts", StatusPassed},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(examples, tc.file))
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			report, err := VerifySource(context.Background(), intent, src, DefaultRulePack())
			if err != nil {
				t.Fatalf("VerifySource: %v", err)
			}
			if report.Status != tc.want {
				t.Errorf("%s: status = %s, want %s", tc.file, report.Status, tc.want)
			}
		})
	}
}

// End-to-end: behavior extraction feeds the comparator, so a source with fewer
// branches than the intent declares surfaces a missing_behavior mismatch.
func TestVerifySource_MissingBehaviorFromExtraction(t *testing.T) {
	intent := mustLoad(t, `
kind: FunctionIntent
name: f
language: typescript
inputs:
  - name: x
    type: number
returns:
  type: string
sideEffects: []
behavior:
  - when: x is negative
    then: return neg
  - when: x is zero
    then: return zero
`)
	src := `export function f(x: number): string {
  if (x < 0) { return "neg"; }
  return "pos";
}`
	report, err := VerifySource(context.Background(), intent, []byte(src), DefaultRulePack())
	if err != nil {
		t.Fatalf("VerifySource: %v", err)
	}
	if findMismatch(report.Mismatches, MismatchMissingBehavior) == nil {
		t.Errorf("expected missing_behavior from extraction, got %+v", report.Mismatches)
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
