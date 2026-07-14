package mutation

import (
	"context"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/lift"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
)

type fixtureObserver struct{ drop string }

func (o fixtureObserver) Observe(_ context.Context, lowered *recipe.ImplementationRecipe, source string) (*lift.Unit, error) {
	observed := &iir.FunctionIntent{Kind: iir.KindFunctionIntent, Name: lowered.Signature.Name, Language: "typescript", Origin: iir.OriginObserved, Visibility: iir.VisibilityPublic, Inputs: []iir.Param{}, Returns: iir.Return{Type: lowered.Signature.ReturnType, Explicit: true}, Behavior: []iir.BehaviorClause{}, SideEffects: []iir.SideEffect{}, FailureModes: []iir.FailureMode{}, Constraints: []string{}}
	for _, input := range lowered.Signature.Parameters {
		observed.Inputs = append(observed.Inputs, iir.Param{Name: input.Name, Type: input.Type})
	}
	for _, effect := range lowered.Effects {
		if effect.Required && effect.Name != o.drop && strings.Contains(source, effect.Name+"()") {
			observed.SideEffects = append(observed.SideEffects, iir.SideEffect{Name: effect.Name, Kind: effect.Kind})
		}
	}
	for _, failure := range lowered.Failures {
		if failure.Strategy != "policy" && failure.Code != o.drop && strings.Contains(source, failure.Code) {
			observed.FailureModes = append(observed.FailureModes, iir.FailureMode{Code: failure.Code, Kind: failure.Strategy})
		}
	}
	return &lift.Unit{NodeID: "fixture", Language: "typescript", SchemaVersion: lift.SchemaVersionV1, Observed: observed, Claims: []lift.Claim{}, Evidence: []lift.Evidence{{Path: "fixture.ts", StartByte: 0, EndByte: len(source), Basis: "fixture"}}, Coverage: lift.CoverageModeled}, nil
}

func mutationPlan(t *testing.T) *plan.SemanticPlan {
	t.Helper()
	intent, err := iir.LoadIntent([]byte(`kind: FunctionIntent
name: updateCustomer
language: typescript
origin: declared
visibility: public
inputs:
  - name: input
    type: UpdateCustomerInput
returns:
  type: Customer
sideEffects:
  - name: repository.save
    kind: db
failureModes:
  - code: ProviderError
    kind: propagated
constraints: []
`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("project", plan.SemanticUnit{ID: "customer-update", CanonicalID: "customer.update", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	p.Lifecycle = plan.LifecycleResolved
	p.Claims = []plan.SemanticClaim{{ID: "mutation", Kind: "effect.mutation", Statement: "customer update", State: plan.KnowledgeObserved, Evidence: []plan.Evidence{{ID: "mutation-evidence", Source: "fixture", Producer: "test", Explanation: "fixture"}}}, {ID: "provider", Kind: "failure.propagated", Statement: "ProviderError", State: plan.KnowledgeObserved, Evidence: []plan.Evidence{{ID: "provider-evidence", Source: "fixture", Producer: "test", Explanation: "fixture"}}}}
	return p
}

func workflow(observer Observer) Workflow {
	return Workflow{Renderer: recipe.TypeScriptEmitter{}, Observer: observer, Rules: iir.DefaultRulePack(), Profile: recipe.DefaultProfile("typescript"), Policies: MutationPolicies()}
}

func TestWorkflowAcceptsModeledMutation(t *testing.T) {
	result, err := workflow(fixtureObserver{}).Execute(context.Background(), mutationPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusAccepted || result.Recipe == nil || result.Report == nil || result.Observed == nil || !strings.Contains(result.Source, "audit.publish") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestWorkflowRejectsMissingAuditWithTargetedGuidance(t *testing.T) {
	result, err := workflow(fixtureObserver{drop: "audit.publish"}).Execute(context.Background(), mutationPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusRejected || !contains(result.Diagnostics, "Implement required effect audit.publish") {
		t.Fatalf("missing audit must be targeted: %+v", result)
	}
}

func TestWorkflowRejectsMissingProviderFailure(t *testing.T) {
	result, err := workflow(fixtureObserver{drop: "ProviderError"}).Execute(context.Background(), mutationPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusRejected || !contains(result.Diagnostics, "Implement required failure behavior ProviderError") {
		t.Fatalf("missing failure must be targeted: %+v", result)
	}
}

func TestWorkflowLeavesPartialLiftConditional(t *testing.T) {
	partial := fixtureObserver{}
	result, err := workflow(partialObserver{partial}).Execute(context.Background(), mutationPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusConditional || !contains(result.Diagnostics, "partial or unsupported") {
		t.Fatalf("partial lift must not pass: %+v", result)
	}
}

type partialObserver struct{ fixtureObserver }

func (o partialObserver) Observe(ctx context.Context, lowered *recipe.ImplementationRecipe, source string) (*lift.Unit, error) {
	unit, err := o.fixtureObserver.Observe(ctx, lowered, source)
	if unit != nil {
		unit.Coverage = lift.CoveragePartial
	}
	return unit, err
}

func contains(values []string, part string) bool {
	for _, value := range values {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}
