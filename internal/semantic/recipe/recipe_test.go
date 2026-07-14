package recipe

import (
	"context"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

func resolvedMutationPlan(t *testing.T) *plan.SemanticPlan {
	t.Helper()
	intent, err := iir.LoadIntent([]byte(`kind: FunctionIntent
name: updateCustomer
language: typescript
inputs:
  - name: input
    type: UpdateCustomerInput
returns:
  type: Customer
behavior:
  - when: authorized
    then: persist customer before publishing audit event
sideEffects:
  - name: repository.save
    kind: db
  - name: audit.publish
    kind: log
failureModes:
  - code: ProviderError
    kind: propagated
constraints:
  - domain services never throw
`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("project", plan.SemanticUnit{ID: "customer-update", CanonicalID: "customer.update", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	p.Lifecycle = plan.LifecycleResolved
	p.Bindings = []plan.SymbolBinding{{ID: "binding-repository", Role: "repository", CanonicalID: "customerRepository", State: plan.KnowledgeResolved, Evidence: []plan.Evidence{{ID: "binding-evidence", Source: "graph", Producer: "test", Explanation: "fixture"}}}}
	p.Claims = []plan.SemanticClaim{
		{ID: "claim-save", Kind: "effect.db", Statement: "repository.save", State: plan.KnowledgeObserved, Evidence: []plan.Evidence{{ID: "claim-save-evidence", Source: "semantic", Producer: "test", Explanation: "fixture"}}},
		{ID: "claim-audit", Kind: "effect.log", Statement: "audit.publish", State: plan.KnowledgeObserved, Evidence: []plan.Evidence{{ID: "claim-audit-evidence", Source: "semantic", Producer: "test", Explanation: "fixture"}}},
		{ID: "claim-failure", Kind: "failure.propagated", Statement: "ProviderError", State: plan.KnowledgeObserved, Evidence: []plan.Evidence{{ID: "claim-failure-evidence", Source: "semantic", Producer: "test", Explanation: "fixture"}}},
	}
	p.Obligations = []plan.Obligation{
		{ID: "obligation-audit", Kind: "audit", Requirement: "audit.publish", State: plan.KnowledgeDeclared, Evidence: []plan.Evidence{{ID: "obligation-audit-evidence", Source: "policy", Producer: "test", Explanation: "fixture"}}},
		{ID: "obligation-failure", Kind: "failure.wrap", Requirement: "wrap provider error", State: plan.KnowledgeDeclared, Evidence: []plan.Evidence{{ID: "obligation-failure-evidence", Source: "policy", Producer: "test", Explanation: "fixture"}}},
	}
	return p
}

func TestLowerProducesStableTraceableRecipe(t *testing.T) {
	p := resolvedMutationPlan(t)
	first, diagnostics, err := Lower(p, DefaultProfile("typescript"))
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("Lower() = %v, diagnostics = %+v", err, diagnostics)
	}
	second, _, err := Lower(p, DefaultProfile("typescript"))
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := MarshalCanonical(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := MarshalCanonical(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) || first.ID != second.ID {
		t.Fatalf("equivalent plans must lower byte-stably:\n%s\n%s", firstJSON, secondJSON)
	}
	if len(first.Imports) != 1 || first.Imports[0].PlanRecordID != "binding-repository" {
		t.Fatalf("resolved repository binding missing from imports: %+v", first.Imports)
	}
	if !hasEffect(first.Effects, "audit.publish", "audit") {
		t.Fatalf("audit policy missing from effects: %+v", first.Effects)
	}
	if !hasFailure(first.Failures, "ProviderError", "propagated") || !hasFailure(first.Failures, "wrap provider error", "policy") {
		t.Fatalf("failure policies missing from recipe: %+v", first.Failures)
	}
	if !hasConstraint(first.Constraints, "obligation-audit") {
		t.Fatalf("policy obligation missing from constraints: %+v", first.Constraints)
	}
	if len(first.EvidenceRefs) == 0 || len(first.UnresolvedQuestions) != 0 {
		t.Fatalf("unexpected traceability fields: %+v", first)
	}
}

func TestLowerRejectsUnresolvedPlan(t *testing.T) {
	p := resolvedMutationPlan(t)
	p.OpenQuestions = []plan.OpenQuestion{{ID: "approval", Prompt: "approve", Blocking: true, State: plan.KnowledgeUnknown, Evidence: []plan.Evidence{{ID: "approval-evidence", Source: "policy", Producer: "test", Explanation: "fixture"}}, Candidates: []plan.Candidate{}}}
	p.Lifecycle = plan.LifecycleResolving
	if _, _, err := Lower(p, DefaultProfile("typescript")); err == nil {
		t.Fatal("expected unresolved plan rejection")
	}
}

func TestTypeScriptEmitterReturnsRecipeTrace(t *testing.T) {
	recipe, _, err := Lower(resolvedMutationPlan(t), DefaultProfile("typescript"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := (TypeScriptEmitter{}).Render(context.Background(), recipe)
	if err != nil {
		t.Fatal(err)
	}
	if result.RecipeID != recipe.ID || result.Renderer == "" || !strings.Contains(result.Source, "customerRepository") || !strings.Contains(result.Source, "audit.publish") {
		t.Fatalf("renderer result is not traceable: %+v", result)
	}
}

func TestLowerReportsUnsupportedTarget(t *testing.T) {
	p := resolvedMutationPlan(t)
	p.Unit.Language, p.Intent.Language = "go", "go"
	_, diagnostics, err := Lower(p, DefaultProfile("go"))
	if err == nil || len(diagnostics) != 1 || diagnostics[0].Code != "unsupported_lowering" {
		t.Fatalf("expected unsupported target diagnostic, got err=%v diagnostics=%+v", err, diagnostics)
	}
}

func hasEffect(effects []Effect, name, kind string) bool {
	for _, effect := range effects {
		if effect.Name == name && effect.Kind == kind {
			return true
		}
	}
	return false
}

func hasFailure(failures []Failure, code, strategy string) bool {
	for _, failure := range failures {
		if failure.Code == code && failure.Strategy == strategy {
			return true
		}
	}
	return false
}

func hasConstraint(constraints []Constraint, id string) bool {
	for _, constraint := range constraints {
		if constraint.PlanRecordID == id {
			return true
		}
	}
	return false
}
