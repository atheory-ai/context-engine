package resolve

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

type fakeReader struct {
	canonical map[string]*core.Node
	suffix    map[string][]core.Node
	edges     map[core.NodeID][]core.EdgeWithWeight
	nodes     map[core.NodeID]*core.Node
}

func (f *fakeReader) GetNodeByCanonicalID(_ context.Context, _ core.ProjectID, canonicalID string) (*core.Node, error) {
	return f.canonical[canonicalID], nil
}

func (f *fakeReader) GetNodesBySuffix(_ context.Context, _ core.ProjectID, suffix string, _ int) ([]core.Node, error) {
	return f.suffix[suffix], nil
}

func (f *fakeReader) GetEdgesFrom(_ context.Context, _ core.ProjectID, nodeID core.NodeID) ([]core.EdgeWithWeight, error) {
	return f.edges[nodeID], nil
}

func (f *fakeReader) GetNode(_ context.Context, _ core.ProjectID, nodeID core.NodeID) (*core.Node, error) {
	return f.nodes[nodeID], nil
}

func resolutionPlan(t *testing.T, unitNodeID core.NodeID, question plan.OpenQuestion) *plan.SemanticPlan {
	t.Helper()
	intent, err := iir.LoadIntent([]byte(`
kind: FunctionIntent
name: updateCustomer
language: typescript
returns:
  type: Customer
`))
	if err != nil {
		t.Fatal(err)
	}
	semanticPlan, err := plan.NewPlan("project", plan.SemanticUnit{
		ID: "customer-update", NodeID: unitNodeID, CanonicalID: "requested.updateCustomer", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{},
	}, intent)
	if err != nil {
		t.Fatal(err)
	}
	semanticPlan.OpenQuestions = []plan.OpenQuestion{question}
	if err := semanticPlan.Validate(); err != nil {
		t.Fatal(err)
	}
	return semanticPlan
}

func question(id string) plan.OpenQuestion {
	return plan.OpenQuestion{
		ID: id, Prompt: "Resolve " + id, Blocking: true, State: plan.KnowledgeUnknown,
		Evidence:   []plan.Evidence{{ID: id + "-evidence", Source: "user", Producer: "test", Confidence: plan.ConfidenceHigh, Explanation: "fixture requirement"}},
		Candidates: []plan.Candidate{},
	}
}

func node(id, canonical string) *core.Node {
	return &core.Node{ID: core.NodeID(id), ProjectID: "project", Type: core.NodeTypeSymbol, CanonicalID: canonical, Label: canonical}
}

func TestResolve_CanonicalMatchProducesBindingAndDecision(t *testing.T) {
	repository := node("repository", "CustomerRepository")
	reader := &fakeReader{canonical: map[string]*core.Node{"CustomerRepository": repository}, suffix: map[string][]core.Node{}, edges: map[core.NodeID][]core.EdgeWithWeight{}, nodes: map[core.NodeID]*core.Node{repository.ID: repository}}
	resolver, err := New(reader, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	semanticPlan := resolutionPlan(t, "service", question("binding-CustomerRepository"))
	report, err := resolver.Resolve(context.Background(), semanticPlan)
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].Outcome != OutcomeResolved || report.Results[0].Selected == nil {
		t.Fatalf("result = %+v", report.Results[0])
	}
	if report.Plan.Revision != 2 || len(report.Plan.OpenQuestions) != 0 || len(report.Plan.Bindings) != 1 || len(report.Plan.Decisions) != 1 {
		t.Fatalf("resolved plan = %+v", report.Plan)
	}
	if report.Plan.Bindings[0].NodeID != repository.ID || report.Plan.Lifecycle != plan.LifecycleResolved {
		t.Fatalf("binding/lifecycle = %+v / %q", report.Plan.Bindings[0], report.Plan.Lifecycle)
	}
}

func TestResolve_RelationshipMatchResolvesWhenCanonicalIsAbsent(t *testing.T) {
	repository := node("repository", "CustomerRepository")
	serviceID := core.NodeID("service")
	reader := &fakeReader{
		canonical: map[string]*core.Node{}, suffix: map[string][]core.Node{}, nodes: map[core.NodeID]*core.Node{repository.ID: repository},
		edges: map[core.NodeID][]core.EdgeWithWeight{serviceID: {{Edge: core.Edge{TargetID: repository.ID}}}},
	}
	resolver, _ := New(reader, 80, 20)
	report, err := resolver.Resolve(context.Background(), resolutionPlan(t, serviceID, question("binding-repository")))
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].Outcome != OutcomeResolved || report.Results[0].Selected.Score != 0.85 {
		t.Fatalf("relationship result = %+v", report.Results[0])
	}
}

func TestResolve_PreservesAmbiguousMissingAndFallbackCandidates(t *testing.T) {
	serviceID := core.NodeID("service")
	left := node("repo-left", "CustomerRepository")
	right := node("repo-right", "LegacyRepository")
	reader := &fakeReader{
		canonical: map[string]*core.Node{}, nodes: map[core.NodeID]*core.Node{left.ID: left, right.ID: right},
		edges:  map[core.NodeID][]core.EdgeWithWeight{serviceID: {{Edge: core.Edge{TargetID: left.ID}}, {Edge: core.Edge{TargetID: right.ID}}}},
		suffix: map[string][]core.Node{"fallback": {*node("fallback", "FallbackRepository")}},
	}
	resolver, _ := New(reader, 80, 20)

	ambiguous, err := resolver.Resolve(context.Background(), resolutionPlan(t, serviceID, question("binding-repository")))
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous.Results[0].Outcome != OutcomeAmbiguous || len(ambiguous.Plan.OpenQuestions[0].Candidates) != 2 {
		t.Fatalf("ambiguous = %+v", ambiguous)
	}

	fallback, err := resolver.Resolve(context.Background(), resolutionPlan(t, serviceID, question("binding-fallback")))
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Results[0].Outcome != OutcomeIncompatible || len(fallback.Plan.OpenQuestions[0].Candidates) != 1 {
		t.Fatalf("fallback = %+v", fallback)
	}

	missing, err := resolver.Resolve(context.Background(), resolutionPlan(t, serviceID, question("binding-missing")))
	if err != nil {
		t.Fatal(err)
	}
	if missing.Results[0].Outcome != OutcomeMissing || len(missing.Plan.OpenQuestions[0].Candidates) != 0 {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestResolve_IsDeterministic(t *testing.T) {
	repository := node("repository", "CustomerRepository")
	reader := &fakeReader{canonical: map[string]*core.Node{"CustomerRepository": repository}, suffix: map[string][]core.Node{}, edges: map[core.NodeID][]core.EdgeWithWeight{}, nodes: map[core.NodeID]*core.Node{repository.ID: repository}}
	resolver, _ := New(reader, 80, 20)
	semanticPlan := resolutionPlan(t, "service", question("binding-CustomerRepository"))
	first, err := resolver.Resolve(context.Background(), semanticPlan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(context.Background(), semanticPlan)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := plan.MarshalCanonical(first.Plan)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := plan.MarshalCanonical(second.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("resolution output differs\nfirst: %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestNew_Guards(t *testing.T) {
	if _, err := New(nil, 0, 0); err == nil {
		t.Fatal("expected nil reader error")
	}
	if _, err := New(&fakeReader{}, 101, 0); err == nil {
		t.Fatal("expected invalid threshold error")
	}
}
