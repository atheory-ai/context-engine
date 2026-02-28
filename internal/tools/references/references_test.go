package references

import (
	"context"
	"testing"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/tools/shared"
)

type mockSubstrate struct {
	shared.NilSubstrate
	refs map[core.NodeID][]core.ReferenceResult
}

func (m *mockSubstrate) GetReferences(_ context.Context, _ core.ProjectID, nodeID core.NodeID) ([]core.ReferenceResult, error) {
	return m.refs[nodeID], nil
}

// TestActivate_ExplicitPredicate verifies predicate-driven activation.
func TestActivate_ExplicitPredicate(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{Predicates: map[string]string{"references": "true"}}
	if !tool.Activate(ir) {
		t.Error("expected Activate=true for references predicate")
	}
}

// TestActivate_ThinkingModeSymbol verifies implicit activation in thinking mode.
func TestActivate_ThinkingModeSymbol(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{
		Mode:    core.IRModeThinking,
		Anchors: []core.AnchorRef{{Type: "symbol", ID: "Foo", Confidence: "high"}},
	}
	if !tool.Activate(ir) {
		t.Error("expected Activate=true for thinking mode with symbol anchor")
	}
}

// TestActivate_DirectModeNoActivation verifies no implicit activation in direct mode.
func TestActivate_DirectModeNoActivation(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{
		Mode:    core.IRModeDirect,
		Anchors: []core.AnchorRef{{Type: "symbol", ID: "Foo", Confidence: "high"}},
	}
	if tool.Activate(ir) {
		t.Error("expected Activate=false for direct mode without explicit predicate")
	}
}

// TestExecute_GroupsByEdgeType verifies grouping and emission content.
func TestExecute_GroupsByEdgeType(t *testing.T) {
	nodeA := core.Node{ID: "A", Type: core.NodeTypeSymbol, Label: "FuncA", CanonicalID: "pkg.FuncA"}
	callerB := core.Node{ID: "B", Type: core.NodeTypeSymbol, Label: "FuncB", CanonicalID: "pkg.FuncB"}
	importerC := core.Node{ID: "C", Type: core.NodeTypeNamespace, Label: "pkg2", CanonicalID: "pkg2"}

	sub := &mockSubstrate{
		refs: map[core.NodeID][]core.ReferenceResult{
			"A": {
				{Node: core.NodeWithActivation{Node: callerB, Activation: 0.9}, EdgeType: "calls", Weight: 0.9},
				{Node: core.NodeWithActivation{Node: importerC, Activation: 0.5}, EdgeType: "imports", Weight: 0.7},
			},
		},
	}

	tool := New(sub)
	req := core.ToolRequest{
		ProjectID: "test",
		IR:        core.IR{Mode: core.IRModeThinking},
		Anchors:   []core.Anchor{{Node: &nodeA}},
	}

	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(result.Emissions) == 0 {
		t.Fatal("expected at least one emission")
	}
	content := result.Emissions[0].Content
	if !containsStr(content, "FuncA") {
		t.Errorf("expected symbol name in emission, got: %s", content)
	}
	if !containsStr(content, "calls") {
		t.Errorf("expected 'calls' edge type in emission")
	}
}

// TestExecute_SkipsNonSymbolNonNamespace verifies filtering.
func TestExecute_SkipsNonSymbolNonNamespace(t *testing.T) {
	conceptNode := core.Node{ID: "C", Type: core.NodeTypeConcept, Label: "auth"}
	tool := New(&mockSubstrate{})
	req := core.ToolRequest{
		ProjectID: "test",
		Anchors:   []core.Anchor{{Node: &conceptNode}},
	}
	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(result.Emissions) > 0 {
		t.Error("expected no emissions for concept anchor")
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
