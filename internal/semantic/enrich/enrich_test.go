package enrich

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

type fakeReader struct {
	nodes   map[core.NodeID]*core.Node
	edges   []core.EdgeWithWeight
	callers []core.NodeWithActivation
}

func (f *fakeReader) GetEdgesFrom(context.Context, core.ProjectID, core.NodeID) ([]core.EdgeWithWeight, error) {
	return f.edges, nil
}
func (f *fakeReader) GetNode(_ context.Context, _ core.ProjectID, id core.NodeID) (*core.Node, error) {
	return f.nodes[id], nil
}
func (f *fakeReader) GetCallers(context.Context, core.ProjectID, core.NodeID, int) ([]core.NodeWithActivation, error) {
	return f.callers, nil
}

type fakeObservations struct{ intent *iir.FunctionIntent }

func (f fakeObservations) ObservedIntent(context.Context, core.ProjectID, core.NodeID) (*iir.FunctionIntent, error) {
	return f.intent, nil
}

func fixturePlan(t *testing.T) *plan.SemanticPlan {
	t.Helper()
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: update\nlanguage: typescript\nreturns:\n  type: Customer\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("project", plan.SemanticUnit{ID: "update", NodeID: "service", CanonicalID: "CustomerService.update", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnrich_AddsStructuralSemanticHeuristicAndUnknownClaims(t *testing.T) {
	repository := &core.Node{ID: "repo", ProjectID: "project", CanonicalID: "CustomerRepository"}
	caller := core.NodeWithActivation{Node: core.Node{ID: "caller", ProjectID: "project", CanonicalID: "CustomerAPI.update"}}
	intent := &iir.FunctionIntent{Kind: iir.KindFunctionIntent, Name: "update", Language: "typescript", Origin: iir.OriginObserved, Visibility: iir.VisibilityPublic,
		SideEffects:  []iir.SideEffect{{Name: "repository.save", Kind: iir.EffectDB, Basis: iir.BasisResolved}, {Name: "audit.emit"}},
		FailureModes: []iir.FailureMode{{Code: "ProviderFailure", Kind: iir.FailurePropagated}},
	}
	e, err := New(&fakeReader{nodes: map[core.NodeID]*core.Node{repository.ID: repository}, edges: []core.EdgeWithWeight{{Edge: core.Edge{TargetID: repository.ID}}}, callers: []core.NodeWithActivation{caller}}, fakeObservations{intent}, 1)
	if err != nil {
		t.Fatal(err)
	}
	next, err := e.Enrich(context.Background(), fixturePlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != 2 || len(next.Claims) < 5 {
		t.Fatalf("enriched plan = %+v", next)
	}
	seen := map[string]plan.KnowledgeState{}
	for _, claim := range next.Claims {
		seen[claim.Kind] = claim.State
	}
	if seen["boundary.repository"] != plan.KnowledgeObserved || seen["effect.db"] != plan.KnowledgeObserved || seen["effect.mutation"] != plan.KnowledgeInferred || seen["failure.propagated"] != plan.KnowledgeObserved {
		t.Fatalf("claims = %+v", seen)
	}
}

func TestEnrich_MarksMissingObservationUnknown(t *testing.T) {
	e, err := New(&fakeReader{nodes: map[core.NodeID]*core.Node{}}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	next, err := e.Enrich(context.Background(), fixturePlan(t))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, claim := range next.Claims {
		if claim.Kind == "unknown" && claim.State == plan.KnowledgeUnknown {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown claim: %+v", next.Claims)
	}
}
