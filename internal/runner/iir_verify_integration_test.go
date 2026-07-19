package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
)

// These are the IIR verify/repair/generate-round-trip tests that used to live in
// internal/iir. They need real source extraction, which now runs through the
// plugin lift (NewIIRExtractor) — hence they live here and skip when the default
// plugins aren't built (run `make bundle-default-plugins`).

var (
	iirExtOnce sync.Once
	iirExt     iir.Extractor
)

// requireIIRExtractor lazily builds a plugin-backed extractor once, skipping the
// test if the default plugins aren't available in this build.
func requireIIRExtractor(t *testing.T) iir.Extractor {
	t.Helper()
	iirExtOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ce-iir-extract-*")
		if err != nil {
			return
		}
		cfg := &config.Config{}
		cfg.DataDir = dir
		ch := core.NewAppChannels()
		ext, _, err := NewIIRExtractor(context.Background(), cfg, &ch) // cleanup leaked until process exit
		if err != nil {
			return
		}
		res, err := ext.Extract(context.Background(), iir.ExtractionInput{
			Language: "typescript",
			Source:   []byte("export function probe(): void {}"),
			Target:   "probe",
		})
		if err == nil && res.Function != nil {
			iirExt = ext
		}
	})
	if iirExt == nil {
		t.Skip("default plugins not built — run `make bundle-default-plugins`")
	}
	return iirExt
}

// ── test helpers (mirrors of the internal/iir test helpers) ─────────────────

func mustLoad(t *testing.T, doc string) *iir.FunctionIntent {
	t.Helper()
	intent, err := iir.LoadIntent([]byte(doc))
	if err != nil {
		t.Fatalf("LoadIntent: %v", err)
	}
	return intent
}

func findMismatch(ms []iir.Mismatch, kind iir.MismatchKind) *iir.Mismatch {
	for i := range ms {
		if ms[i].Kind == kind {
			return &ms[i]
		}
	}
	return nil
}

func errorMismatches(report *iir.Report) []iir.Mismatch {
	var out []iir.Mismatch
	for _, m := range report.Mismatches {
		if m.Severity == iir.SeverityError {
			out = append(out, m)
		}
	}
	return out
}

func path(s string) *iir.Expr { return &iir.Expr{Op: "path", Text: s} }
func lit(s string) *iir.Expr  { return &iir.Expr{Op: "lit", Text: s} }
func bin(op string, l, r *iir.Expr) *iir.Expr {
	return &iir.Expr{Op: op, Args: []*iir.Expr{l, r}}
}

func roundTrip(t *testing.T, ext iir.Extractor, intent *iir.FunctionIntent) (string, *iir.Report) {
	t.Helper()
	src, err := iir.GenerateFunction(intent)
	if err != nil {
		t.Fatalf("GenerateFunction: %v", err)
	}
	report, err := iir.VerifySource(context.Background(), ext, intent, []byte(src), iir.DefaultRulePack())
	if err != nil {
		t.Fatalf("VerifySource on generated source: %v\n--- source ---\n%s", err, src)
	}
	return src, report
}

// ── generate round-trip tests ───────────────────────────────────────────────

func TestGenerate_ResultStrategyRoundTrips(t *testing.T) {
	ext := requireIIRExtractor(t)
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
	src, report := roundTrip(t, ext, intent)
	if errs := errorMismatches(report); len(errs) != 0 {
		t.Errorf("round-trip produced error mismatches: %+v", errs)
	}
	if report.Status != iir.StatusInconclusive {
		t.Errorf("round-trip status = %s, want inconclusive", report.Status)
	}
	if !strings.HasPrefix(src, "export function validateDonationAmount(") {
		t.Errorf("unexpected signature:\n%s", src)
	}
}

func TestGenerate_ThrowStrategyRoundTripsFailureModes(t *testing.T) {
	ext := requireIIRExtractor(t)
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
	_, report := roundTrip(t, ext, intent)
	if errs := errorMismatches(report); len(errs) != 0 {
		t.Errorf("round-trip error mismatches: %+v", errs)
	}
	if findMismatch(report.Mismatches, iir.MismatchChangedFailureMode) != nil {
		t.Errorf("expected failure mode to round-trip via throw: %+v", report.Mismatches)
	}
}

func TestGenerate_DeclaredSideEffectsRoundTrip(t *testing.T) {
	ext := requireIIRExtractor(t)
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
	_, report := roundTrip(t, ext, intent)
	if m := findMismatch(report.Mismatches, iir.MismatchUndeclaredEffect); m != nil {
		t.Errorf("declared side effect should not be undeclared: %+v", m)
	}
	if m := findMismatch(report.Mismatches, iir.MismatchUndetectedEffect); m != nil {
		t.Errorf("declared side effect should be detected in generated source: %+v", m)
	}
}

func TestGenerate_BehaviorCountRoundTrips(t *testing.T) {
	ext := requireIIRExtractor(t)
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
	_, report := roundTrip(t, ext, intent)
	if findMismatch(report.Mismatches, iir.MismatchMissingBehavior) != nil ||
		findMismatch(report.Mismatches, iir.MismatchExtraBehavior) != nil {
		t.Errorf("behavior count should round-trip: %+v", report.Mismatches)
	}
}

func TestGenerate_BehaviorWhenExprRoundTrips(t *testing.T) {
	ext := requireIIRExtractor(t)
	intent := &iir.FunctionIntent{
		Kind:     iir.KindFunctionIntent,
		Name:     "check",
		Language: "typescript",
		Inputs:   []iir.Param{{Name: "id", Type: "string"}},
		Returns:  iir.Return{Type: "boolean", Explicit: true},
		Behavior: []iir.BehaviorClause{{
			When:     "id is null",
			Then:     "return false",
			WhenExpr: bin("===", path("id"), lit("null")),
		}},
	}
	src, report := roundTrip(t, ext, intent)
	if !strings.Contains(src, "if (id === null)") {
		t.Errorf("expected a real guard, got:\n%s", src)
	}
	if m := findMismatch(report.Mismatches, iir.MismatchBehaviorContent); m != nil {
		t.Errorf("WhenExpr should round-trip without a content mismatch: %+v", m)
	}
}

func TestGenerate_RespectsRulePacks(t *testing.T) {
	ext := requireIIRExtractor(t)
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
	_, report := roundTrip(t, ext, intent)
	for _, r := range report.RuleResults {
		if r.Status == iir.RuleFailed {
			t.Errorf("generated code violates rule %s: %s", r.ID, r.Message)
		}
	}
}

// ── verify tests ─────────────────────────────────────────────────────────────

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
	ext := requireIIRExtractor(t)
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
	report, err := iir.VerifySource(context.Background(), ext, intent, []byte(src), iir.DefaultRulePack())
	if err != nil {
		t.Fatalf("VerifySource: %v", err)
	}
	if report.Status != iir.StatusPassed {
		t.Errorf("status = %s, want passed\nmismatches: %+v", report.Status, report.Mismatches)
	}
}

func TestVerifySource_FailsOnUndeclaredSideEffect(t *testing.T) {
	ext := requireIIRExtractor(t)
	intent := mustLoad(t, donationIntentYAML)
	src := `
import { db } from "./db";
import { ok } from "./result";
export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> {
  db.query("insert audit");
  return ok(amount);
}
`
	report, err := iir.VerifySource(context.Background(), ext, intent, []byte(src), iir.DefaultRulePack())
	if err != nil {
		t.Fatalf("VerifySource: %v", err)
	}
	if report.Status != iir.StatusFailed {
		t.Fatalf("status = %s, want failed", report.Status)
	}
	if findMismatch(report.Mismatches, iir.MismatchUndeclaredEffect) == nil {
		t.Errorf("expected undeclared_side_effect mismatch, got %+v", report.Mismatches)
	}
	if len(report.RepairTargets) == 0 {
		t.Error("failed report must include repair targets")
	}
}

func TestVerifySource_MissingBehaviorFromExtraction(t *testing.T) {
	ext := requireIIRExtractor(t)
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
	report, err := iir.VerifySource(context.Background(), ext, intent, []byte(src), iir.DefaultRulePack())
	if err != nil {
		t.Fatalf("VerifySource: %v", err)
	}
	if findMismatch(report.Mismatches, iir.MismatchMissingBehavior) == nil {
		t.Errorf("expected missing_behavior from extraction, got %+v", report.Mismatches)
	}
}

func TestVerify_ExampleFiles(t *testing.T) {
	ext := requireIIRExtractor(t)
	examples := filepath.Join("..", "..", "examples")
	intent, err := iir.LoadIntentFile(filepath.Join(examples, "validateDonationAmount.iir.yaml"))
	if err != nil {
		t.Fatalf("load example intent: %v", err)
	}
	cases := []struct {
		file string
		want iir.Status
	}{
		{"function-source-sample.ts", iir.StatusFailed},
		{"function-source-clean.ts", iir.StatusInconclusive},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(examples, tc.file))
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			report, err := iir.VerifySource(context.Background(), ext, intent, src, iir.DefaultRulePack())
			if err != nil {
				t.Fatalf("VerifySource: %v", err)
			}
			if report.Status != tc.want {
				t.Errorf("%s: status = %s, want %s", tc.file, report.Status, tc.want)
			}
		})
	}
}

func TestVerifySource_UnsupportedLanguageErrors(t *testing.T) {
	ext := requireIIRExtractor(t)
	intent := mustLoad(t, donationIntentYAML)
	intent.Language = "cobol" // no plugin frontend
	if _, err := iir.VerifySource(context.Background(), ext, intent, []byte(`identification division.`), iir.DefaultRulePack()); err == nil {
		t.Error("expected an error when no extractor supports the language")
	}
}

// ── repair tests ─────────────────────────────────────────────────────────────

const brokenSource = `
import { db } from "./db";
import { ok } from "./result";
export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> {
  db.query("insert audit");
  return ok(amount);
}
`

func repairIntent(t *testing.T) *iir.FunctionIntent {
	return mustLoad(t, `
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
`)
}

func TestRepairLoop_ConvergesViaRegenerate(t *testing.T) {
	ext := requireIIRExtractor(t)
	intent := repairIntent(t)
	res, err := iir.RepairLoop(context.Background(), ext, intent, brokenSource, iir.DefaultRulePack(), iir.RegenerateStage{}, iir.RepairOptions{})
	if err != nil {
		t.Fatalf("RepairLoop: %v", err)
	}
	if !res.Converged {
		t.Fatalf("expected convergence, got report: %+v", res.FinalReport.Mismatches)
	}
	if res.FinalReport.Status != iir.StatusPassed {
		t.Errorf("final status = %s, want passed", res.FinalReport.Status)
	}
	if len(res.Iterations) < 2 {
		t.Errorf("expected at least one repair iteration, got %d", len(res.Iterations))
	}
	if res.Iterations[0].Report.Status != iir.StatusFailed {
		t.Errorf("first iteration should have failed")
	}
	if !res.Iterations[0].Applied {
		t.Errorf("first proposal should have been applied under auto-approve")
	}
}

func TestRepairLoop_AlreadyPassingNoRepair(t *testing.T) {
	ext := requireIIRExtractor(t)
	intent := repairIntent(t)
	clean := `export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> { return ok(amount); }`
	res, err := iir.RepairLoop(context.Background(), ext, intent, clean, iir.DefaultRulePack(), iir.RegenerateStage{}, iir.RepairOptions{})
	if err != nil {
		t.Fatalf("RepairLoop: %v", err)
	}
	if !res.Converged || len(res.Iterations) != 1 {
		t.Errorf("passing source should converge in one iteration, got %d: converged=%v", len(res.Iterations), res.Converged)
	}
	if res.Iterations[0].Proposed != "" {
		t.Errorf("no repair should be proposed for passing source")
	}
}

// stubStage always proposes the same (still-failing) source, so the loop never
// converges and must stop at MaxIterations.
type stubStage struct{ out string }

func (stubStage) ID() string                                                   { return "stub" }
func (s stubStage) Propose(context.Context, iir.RepairContext) (string, error) { return s.out, nil }

func TestRepairLoop_StopsAtMaxIterations(t *testing.T) {
	ext := requireIIRExtractor(t)
	intent := repairIntent(t)
	stillBroken := `export function validateDonationAmount(amount: Money): ValidationResult<Money> { return ok(amount); }`
	res, err := iir.RepairLoop(context.Background(), ext, intent, stillBroken, iir.DefaultRulePack(),
		stubStage{out: stillBroken}, iir.RepairOptions{MaxIterations: 2})
	if err != nil {
		t.Fatalf("RepairLoop: %v", err)
	}
	if res.Converged {
		t.Error("expected no convergence for a non-repairing stage")
	}
	if len(res.Iterations) != 3 {
		t.Errorf("expected 3 iterations (initial + 2 repairs), got %d", len(res.Iterations))
	}
}

func TestRepairLoop_RejectedProposalStops(t *testing.T) {
	ext := requireIIRExtractor(t)
	intent := repairIntent(t)
	reject := func(int, string, iir.RepairContext) bool { return false }
	res, err := iir.RepairLoop(context.Background(), ext, intent, brokenSource, iir.DefaultRulePack(),
		iir.RegenerateStage{}, iir.RepairOptions{Approve: reject})
	if err != nil {
		t.Fatalf("RepairLoop: %v", err)
	}
	if res.Converged {
		t.Error("expected no convergence when the proposal is rejected")
	}
	if len(res.Iterations) != 1 || res.Iterations[0].Applied {
		t.Errorf("rejected proposal should stop after one iteration, unapplied: %+v", res.Iterations)
	}
}
