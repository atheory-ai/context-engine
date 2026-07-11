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

// The RepairLoop convergence tests need source extraction, so they live in
// internal/runner. This keeps the pure stage-behavior test.
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
