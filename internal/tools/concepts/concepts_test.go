package concepts

import (
	"context"
	"testing"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/tools/shared"
)

type mockSubstrate struct {
	shared.NilSubstrate
	implementors map[core.NodeID][]core.NodeWithActivation
	seeds        map[string]*core.ConceptSeed
}

func (m *mockSubstrate) GetConceptImplementors(_ context.Context, _ core.ProjectID, nodeID core.NodeID) ([]core.NodeWithActivation, error) {
	return m.implementors[nodeID], nil
}
func (m *mockSubstrate) GetConceptSeed(_ context.Context, _ core.ProjectID, term string) (*core.ConceptSeed, error) {
	return m.seeds[term], nil
}

// TestActivate_ExplicitPredicate verifies predicate-driven activation.
func TestActivate_ExplicitPredicate(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{Predicates: map[string]string{"concepts": "true"}}
	if !tool.Activate(ir) {
		t.Error("expected Activate=true for concepts predicate")
	}
}

// TestActivate_ConceptAnchor verifies implicit activation for concept anchors.
func TestActivate_ConceptAnchor(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{
		Anchors: []core.AnchorRef{{Type: "concept", ID: "auth"}},
	}
	if !tool.Activate(ir) {
		t.Error("expected Activate=true for concept anchor")
	}
}

// TestActivate_NoConceptAnchor verifies no implicit activation for non-concept anchors.
func TestActivate_NoConceptAnchor(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{
		Anchors: []core.AnchorRef{{Type: "symbol", ID: "Foo"}},
	}
	if tool.Activate(ir) {
		t.Error("expected Activate=false for symbol-only anchors")
	}
}

// TestExecute_EmitsWithSeedAndImplementors verifies complete emission with seed data.
func TestExecute_EmitsWithSeedAndImplementors(t *testing.T) {
	conceptNode := core.Node{
		ID: "C", Type: core.NodeTypeConcept, Label: "authentication", CanonicalID: "authentication",
	}
	implNode := core.Node{ID: "I", Type: core.NodeTypeSymbol, Label: "AuthService", CanonicalID: "pkg.AuthService"}

	sub := &mockSubstrate{
		implementors: map[core.NodeID][]core.NodeWithActivation{
			"C": {{Node: implNode, Activation: 0.8}},
		},
		seeds: map[string]*core.ConceptSeed{
			"authentication": {
				Term:       "authentication",
				Definition: "Verifying the identity of a user.",
				Related:    []string{"authorization", "session"},
				Synonyms:   []string{"auth"},
			},
		},
	}

	tool := New(sub)
	req := core.ToolRequest{
		ProjectID: "test",
		Anchors:   []core.Anchor{{Node: &conceptNode}},
	}

	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(result.Emissions) == 0 {
		t.Fatal("expected at least one emission")
	}
	content := result.Emissions[0].Content
	if !containsStr(content, "authentication") {
		t.Errorf("expected concept name in emission")
	}
	if !containsStr(content, "Verifying") {
		t.Errorf("expected definition in emission")
	}
	if !containsStr(content, "AuthService") {
		t.Errorf("expected implementor in emission")
	}
}

// TestExecute_SkipsNonConcepts verifies non-concept anchors are skipped.
func TestExecute_SkipsNonConcepts(t *testing.T) {
	symbolNode := core.Node{ID: "S", Type: core.NodeTypeSymbol, Label: "Foo"}
	tool := New(&mockSubstrate{})
	req := core.ToolRequest{
		ProjectID: "test",
		Anchors:   []core.Anchor{{Node: &symbolNode}},
	}
	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(result.Emissions) > 0 {
		t.Error("expected no emissions for symbol anchor")
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
