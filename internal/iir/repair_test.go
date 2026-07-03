package iir

import (
	"context"
	"testing"
)

func repairIntent(t *testing.T) *FunctionIntent {
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

// A source with an undeclared side effect fails verification; the deterministic
// regenerate stage repairs it in one step.
const brokenSource = `
import { analytics } from "./analytics";
import { ok } from "./result";
export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> {
  analytics.track("validated");
  return ok(amount);
}
`

func TestRepairLoop_ConvergesViaRegenerate(t *testing.T) {
	intent := repairIntent(t)
	res, err := RepairLoop(context.Background(), intent, brokenSource, DefaultRulePack(), RegenerateStage{}, RepairOptions{})
	if err != nil {
		t.Fatalf("RepairLoop: %v", err)
	}
	if !res.Converged {
		t.Fatalf("expected convergence, got report: %+v", res.FinalReport.Mismatches)
	}
	if res.FinalReport.Status != StatusPassed {
		t.Errorf("final status = %s, want passed", res.FinalReport.Status)
	}
	// First iteration failed (undeclared side effect); a later one passed.
	if len(res.Iterations) < 2 {
		t.Errorf("expected at least one repair iteration, got %d", len(res.Iterations))
	}
	if res.Iterations[0].Report.Status != StatusFailed {
		t.Errorf("first iteration should have failed")
	}
	if !res.Iterations[0].Applied {
		t.Errorf("first proposal should have been applied under auto-approve")
	}
}

func TestRepairLoop_AlreadyPassingNoRepair(t *testing.T) {
	intent := repairIntent(t)
	clean := `export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> { return ok(amount); }`
	res, err := RepairLoop(context.Background(), intent, clean, DefaultRulePack(), RegenerateStage{}, RepairOptions{})
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

func (stubStage) ID() string                                               { return "stub" }
func (s stubStage) Propose(context.Context, RepairContext) (string, error) { return s.out, nil }

func TestRepairLoop_StopsAtMaxIterations(t *testing.T) {
	intent := repairIntent(t)
	// A source missing the campaign input keeps failing (missing_input error).
	stillBroken := `export function validateDonationAmount(amount: Money): ValidationResult<Money> { return ok(amount); }`
	res, err := RepairLoop(context.Background(), intent, stillBroken, DefaultRulePack(),
		stubStage{out: stillBroken}, RepairOptions{MaxIterations: 2})
	if err != nil {
		t.Fatalf("RepairLoop: %v", err)
	}
	if res.Converged {
		t.Error("expected no convergence for a non-repairing stage")
	}
	// Verified attempts = MaxIterations + 1 (initial + 2 repairs).
	if len(res.Iterations) != 3 {
		t.Errorf("expected 3 iterations (initial + 2 repairs), got %d", len(res.Iterations))
	}
}

func TestRepairLoop_RejectedProposalStops(t *testing.T) {
	intent := repairIntent(t)
	reject := func(int, string, RepairContext) bool { return false }
	res, err := RepairLoop(context.Background(), intent, brokenSource, DefaultRulePack(),
		RegenerateStage{}, RepairOptions{Approve: reject})
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

func TestRegenerateStage_ProposesGeneratedSource(t *testing.T) {
	intent := repairIntent(t)
	got, err := RegenerateStage{}.Propose(context.Background(), RepairContext{Intent: intent})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	want, _ := GenerateFunction(intent)
	if got != want {
		t.Errorf("regenerate stage should return generated source")
	}
}
