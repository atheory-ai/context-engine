package crossproject

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/tools/shared"
)

type mockSubstrate struct {
	shared.NilSubstrate
	orgMatches map[string][]core.OrgMatch
}

func (m *mockSubstrate) FindInOrgGraph(_ context.Context, canonicalID string, _ string) ([]core.OrgMatch, error) {
	return m.orgMatches[canonicalID], nil
}

// TestActivate_ExplicitPredicate verifies predicate-driven activation.
func TestActivate_ExplicitPredicate(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{Predicates: map[string]string{"crossproject": "true"}}
	if !tool.Activate(ir) {
		t.Error("expected Activate=true for crossproject predicate")
	}
}

// TestActivate_TwoConceptAnchors verifies 2+ concept anchors triggers implicit activation.
func TestActivate_TwoConceptAnchors(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{
		Anchors: []core.AnchorRef{
			{Type: "concept", ID: "auth"},
			{Type: "concept", ID: "billing"},
		},
	}
	if !tool.Activate(ir) {
		t.Error("expected Activate=true for 2 concept anchors")
	}
}

// TestActivate_OneConceptAnchorNoActivation verifies single concept does not trigger.
func TestActivate_OneConceptAnchorNoActivation(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{
		Anchors: []core.AnchorRef{{Type: "concept", ID: "auth"}},
	}
	if tool.Activate(ir) {
		t.Error("expected Activate=false for only 1 concept anchor")
	}
}

// TestExecute_EmitsForOrgMatch verifies emission is produced for org graph matches.
func TestExecute_EmitsForOrgMatch(t *testing.T) {
	node := core.Node{
		ID:          "A",
		Type:        core.NodeTypeSymbol,
		Label:       "ProcessPayment",
		CanonicalID: "billing.ProcessPayment",
		ProjectID:   "proj-a",
	}
	orgNode := core.Node{
		ID:          "B",
		Type:        core.NodeTypeSymbol,
		Label:       "ProcessPayment",
		CanonicalID: "billing.ProcessPayment",
		ProjectID:   "proj-b",
	}
	sub := &mockSubstrate{
		orgMatches: map[string][]core.OrgMatch{
			"billing.ProcessPayment": {
				{Node: orgNode, ProjectID: "proj-b", ProjectName: "proj-b", Similarity: 1.0},
			},
		},
	}

	tool := New(sub)
	req := core.ToolRequest{
		ProjectID: "proj-a",
		Anchors:   []core.Anchor{{Node: &node}},
	}

	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(result.Emissions) == 0 {
		t.Fatal("expected at least one emission")
	}
	if !containsStr(result.Emissions[0].Content, "proj-b") {
		t.Errorf("expected emission to reference project name")
	}
}

// TestExecute_FiltersSameProject verifies same-project matches are excluded.
func TestExecute_FiltersSameProject(t *testing.T) {
	node := core.Node{
		ID: "A", Type: core.NodeTypeSymbol, Label: "Foo", CanonicalID: "Foo", ProjectID: "proj-a",
	}
	orgNode := core.Node{
		ID: "B", Type: core.NodeTypeSymbol, Label: "Foo", CanonicalID: "Foo", ProjectID: "proj-a",
	}
	sub := &mockSubstrate{
		orgMatches: map[string][]core.OrgMatch{
			"Foo": {{Node: orgNode, ProjectID: "proj-a", ProjectName: "proj-a", Similarity: 1.0}},
		},
	}

	tool := New(sub)
	req := core.ToolRequest{
		ProjectID: "proj-a",
		Anchors:   []core.Anchor{{Node: &node}},
	}
	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(result.Emissions) > 0 {
		t.Error("expected no emissions — same-project match should be filtered")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
