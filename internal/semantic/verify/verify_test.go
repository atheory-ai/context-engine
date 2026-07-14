package verify

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/lift"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
)

func fixture(t *testing.T) (*plan.SemanticPlan, *recipe.ImplementationRecipe, *lift.Unit) {
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
	r := &recipe.ImplementationRecipe{ID: "recipe-fixture", SchemaVersion: recipe.SchemaVersionV1, PlanRevisionID: p.ID, TargetLanguage: "typescript", Target: recipe.Target{UnitID: "update", Mode: "existing"}, Signature: recipe.Signature{Name: "update", PlanRecordID: "intent"}, Effects: []recipe.Effect{{Name: "audit.publish", Kind: "audit", Required: true, PlanRecordID: "obligation-audit"}}, Failures: []recipe.Failure{{Code: "ProviderError", Strategy: "propagated", PlanRecordID: "claim-failure"}}, Imports: []recipe.Import{}, Steps: []recipe.Step{}, Constraints: []recipe.Constraint{}, RendererProfile: recipe.DefaultProfile("typescript"), EvidenceRefs: []string{}, UnresolvedQuestions: []string{}}
	observed := &lift.Unit{NodeID: "source", Language: "typescript", SchemaVersion: lift.SchemaVersionV1, Coverage: lift.CoverageModeled, Observed: &iir.FunctionIntent{Kind: iir.KindFunctionIntent, Name: "update", Language: "typescript", Origin: iir.OriginObserved, Returns: iir.Return{Type: "void"}, Inputs: []iir.Param{}, Behavior: []iir.BehaviorClause{}, SideEffects: []iir.SideEffect{{Name: "audit.publish", Kind: "audit"}}, FailureModes: []iir.FailureMode{{Code: "ProviderError", Kind: "propagated"}}, Constraints: []string{}}, Claims: []lift.Claim{}, Evidence: []lift.Evidence{{Path: "update.ts", StartByte: 0, EndByte: 10, Basis: "resolved"}}}
	return p, r, observed
}

func TestVerifyPassesOnlyModeledEvidence(t *testing.T) {
	p, r, observed := fixture(t)
	report, err := Verify(p, r, observed)
	if err != nil || report.Status != StatusPassed || len(report.Findings) != 2 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestVerifyMissingMandatoryEffectFailsWithEvidence(t *testing.T) {
	p, r, observed := fixture(t)
	observed.Observed.SideEffects = []iir.SideEffect{}
	report, err := Verify(p, r, observed)
	if err != nil || report.Status != StatusFailed || report.Findings[0].Result != ResultViolated || report.Findings[0].RepairTarget == "" || len(report.Findings[0].Evidence) == 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestVerifyPartialObservationIsInconclusive(t *testing.T) {
	p, r, observed := fixture(t)
	observed.Coverage = lift.CoveragePartial
	report, err := Verify(p, r, observed)
	if err != nil || report.Status != StatusInconclusive || report.Findings[0].Result != ResultConditional {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestVerifyMissingRequiredBoundaryCallIsInconclusiveWithEvidence(t *testing.T) {
	p, r, observed := fixture(t)
	r.Steps = []recipe.Step{{Order: 1, Operation: "boundary call", RequiredCall: "repository.save", PlanRecordID: "binding-repository"}}
	report, err := Verify(p, r, observed)
	if err != nil || report.Status != StatusInconclusive {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	for _, finding := range report.Findings {
		if finding.PlanRecordID == "binding-repository" {
			if finding.Result != ResultUnsupported || finding.RepairTarget == "" || len(finding.Evidence) == 0 {
				t.Fatalf("boundary finding must retain plan/source evidence: %+v", finding)
			}
			return
		}
	}
	t.Fatalf("missing direct-boundary finding: %+v", report.Findings)
}

func TestVerifyPartialEvidenceNeverPassesAcrossPluginLiftLanguages(t *testing.T) {
	for _, language := range []string{"go", "python"} {
		t.Run(language, func(t *testing.T) {
			p, r, observed := fixture(t)
			observed.Language = language
			observed.Observed.Language = language
			observed.Coverage = lift.CoveragePartial
			report, err := Verify(p, r, observed)
			if err != nil || report.Status != StatusInconclusive {
				t.Fatalf("partial %s observation must not pass: report=%+v err=%v", language, report, err)
			}
		})
	}
}
