package repair

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
	semanticverify "github.com/atheory-ai/context-engine/internal/semantic/verify"
)

func fixture(t *testing.T) (*plan.SemanticPlan, *recipe.ImplementationRecipe, Artifact) {
	t.Helper()
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: update\nlanguage: typescript\norigin: declared\nreturns:\n  type: void\nsideEffects: []\nfailureModes: []\nconstraints: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("project", plan.SemanticUnit{ID: "update", CanonicalID: "customer.update", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	p.Lifecycle = plan.LifecycleResolved
	r := &recipe.ImplementationRecipe{ID: "recipe-fixture", SchemaVersion: recipe.SchemaVersionV1, PlanRevisionID: p.ID, TargetLanguage: "typescript", Target: recipe.Target{UnitID: "update", Mode: "existing"}, Signature: recipe.Signature{Name: "update", PlanRecordID: "intent"}, Imports: []recipe.Import{}, Steps: []recipe.Step{}, Effects: []recipe.Effect{}, Failures: []recipe.Failure{}, Constraints: []recipe.Constraint{}, RendererProfile: recipe.DefaultProfile("typescript"), EvidenceRefs: []string{}, UnresolvedQuestions: []string{}}
	return p, r, Artifact{ID: "artifact-a", SourceHash: "source-hash", RendererID: "typescript.deterministic.v1"}
}

func TestMissingAuditCreatesTargetedRecipePatch(t *testing.T) {
	p, r, artifact := fixture(t)
	report := &semanticverify.Report{Status: semanticverify.StatusFailed, PlanRevisionID: p.ID, RecipeID: r.ID, Findings: []semanticverify.Finding{{PlanRecordID: "obligation-audit", Result: semanticverify.ResultViolated, Expected: "required effect audit.publish", RepairTarget: "Implement audit.publish with source evidence."}}}
	proposed, err := Propose(p, r, artifact, nil, report, nil)
	if err != nil {
		t.Fatal(err)
	}
	if proposed.Classification != ImplementationDivergence || len(proposed.Changes) != 1 || proposed.Changes[0].Kind != "recipe_patch" || proposed.Changes[0].Operation != "ensure_effect" || proposed.Changes[0].TargetID != "obligation-audit" {
		t.Fatalf("unexpected repair: %+v", proposed)
	}
}

func TestInconclusiveEvidenceDoesNotRetrySource(t *testing.T) {
	p, r, artifact := fixture(t)
	report := &semanticverify.Report{Status: semanticverify.StatusInconclusive, PlanRevisionID: p.ID, RecipeID: r.ID, Findings: []semanticverify.Finding{}}
	proposed, err := Propose(p, r, artifact, nil, report, nil)
	if err != nil {
		t.Fatal(err)
	}
	if proposed.Classification != InsufficientEvidence || len(proposed.Changes) != 0 || proposed.Status != StatusProposed {
		t.Fatalf("unexpected repair: %+v", proposed)
	}
}

func TestEquivalentRepairIsExhausted(t *testing.T) {
	p, r, artifact := fixture(t)
	report := &semanticverify.Report{Status: semanticverify.StatusFailed, PlanRevisionID: p.ID, RecipeID: r.ID, Findings: []semanticverify.Finding{{PlanRecordID: "claim-audit", Result: semanticverify.ResultViolated, Expected: "required effect audit.publish", RepairTarget: "Implement audit.publish."}}}
	first, err := Propose(p, r, artifact, nil, report, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Propose(p, r, artifact, nil, report, []Plan{*first})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != StatusExhausted {
		t.Fatalf("expected exhausted repair, got %+v", second)
	}
}
