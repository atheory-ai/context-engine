package testplan

import (
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
	"github.com/atheory-ai/context-engine/internal/semantic/repair"
	"strings"
	"testing"
)

func TestLowerMutationCasesAndGaps(t *testing.T) {
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: update\nlanguage: typescript\norigin: declared\nreturns:\n  type: void\nsideEffects: []\nfailureModes: []\nconstraints: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("p", plan.SemanticUnit{ID: "u", CanonicalID: "customer.update", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	p.Lifecycle = plan.LifecycleResolved
	p.Obligations = []plan.Obligation{{ID: "obligation-boundary", Kind: "repository_boundary", Requirement: "use repository", State: plan.KnowledgeDeclared, Evidence: []plan.Evidence{{ID: "e", Source: "policy", Producer: "test", Explanation: "fixture"}}}}
	r := &recipe.ImplementationRecipe{ID: "r", PlanRevisionID: p.ID, Effects: []recipe.Effect{{Name: "audit.publish", Required: true, PlanRecordID: "obligation-audit"}}, Failures: []recipe.Failure{{Code: "ProviderError", Strategy: "propagated", PlanRecordID: "claim-failure"}}}
	out, err := Lower(p, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Cases) != 3 || len(out.Gaps) != 1 || out.Gaps[0].PlanRecordID != "obligation-boundary" {
		t.Fatalf("%+v", out)
	}
	source, err := RenderTypeScript(out)
	if err != nil || !strings.Contains(source, "audit.publish") || !strings.Contains(source, "ProviderError") {
		t.Fatalf("%s %v", source, err)
	}
}

func TestLowerWithRepairsAddsRegressionCase(t *testing.T) {
	p := resolvedPlan(t)
	r := &recipe.ImplementationRecipe{ID: "r", PlanRevisionID: p.ID}
	out, err := LowerWithRepairs(p, r, []repair.Plan{{ID: "repair-a", Changes: []repair.Change{{Kind: "recipe_patch", TargetID: "obligation-audit", Requirement: "required effect audit.publish"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range out.Cases {
		if testCase.Category == "regression" && testCase.ID == "test-regression-repair-a-obligation-audit" {
			return
		}
	}
	t.Fatalf("regression case absent: %+v", out.Cases)
}

func resolvedPlan(t *testing.T) *plan.SemanticPlan {
	t.Helper()
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: update\nlanguage: typescript\norigin: declared\nreturns:\n  type: void\nsideEffects: []\nfailureModes: []\nconstraints: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("p", plan.SemanticUnit{ID: "u", CanonicalID: "customer.update", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	p.Lifecycle = plan.LifecycleResolved
	return p
}
