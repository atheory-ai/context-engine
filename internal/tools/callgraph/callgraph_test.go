package callgraph

import (
	"context"
	"testing"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/tools/shared"
)

// mockSubstrate overrides only the methods the callgraph tool uses.
type mockSubstrate struct {
	shared.NilSubstrate
	callers map[core.NodeID][]core.NodeWithActivation
	callees map[core.NodeID][]core.NodeWithActivation
}

func (m *mockSubstrate) GetCallers(_ context.Context, _ core.ProjectID, nodeID core.NodeID, _ int) ([]core.NodeWithActivation, error) {
	return m.callers[nodeID], nil
}
func (m *mockSubstrate) GetCallees(_ context.Context, _ core.ProjectID, nodeID core.NodeID, _ int) ([]core.NodeWithActivation, error) {
	return m.callees[nodeID], nil
}

func makeSymbolNode(id, label string) core.Node {
	return core.Node{ID: core.NodeID(id), Type: core.NodeTypeSymbol, Label: label, CanonicalID: label}
}

// TestActivate_ExplicitPredicate verifies predicate-driven activation.
func TestActivate_ExplicitPredicate(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{Predicates: map[string]string{"callgraph": "true"}}
	if !tool.Activate(ir) {
		t.Error("expected Activate=true for callgraph predicate")
	}
}

// TestActivate_SymbolAnchorMediumConfidence verifies implicit activation.
func TestActivate_SymbolAnchorMediumConfidence(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{
		Anchors: []core.AnchorRef{{Type: "symbol", ID: "Foo", Confidence: "medium"}},
	}
	if !tool.Activate(ir) {
		t.Error("expected Activate=true for symbol anchor with medium confidence")
	}
}

// TestActivate_LowConfidenceSkipped verifies low-confidence symbols do not activate.
func TestActivate_LowConfidenceSkipped(t *testing.T) {
	tool := New(&mockSubstrate{})
	ir := core.IR{
		Anchors: []core.AnchorRef{{Type: "symbol", ID: "Foo", Confidence: "low"}},
	}
	if tool.Activate(ir) {
		t.Error("expected Activate=false for low-confidence symbol anchor")
	}
}

// TestExecute_EmitsCallgraph verifies emission is produced for a symbol with callers/callees.
func TestExecute_CallersAndCallees(t *testing.T) {
	nodeA := makeSymbolNode("A", "FuncA")
	nodeB := makeSymbolNode("B", "FuncB")
	nodeC := makeSymbolNode("C", "FuncC")

	sub := &mockSubstrate{
		callers: map[core.NodeID][]core.NodeWithActivation{
			"A": {{Node: nodeB, Activation: 0.8}},
		},
		callees: map[core.NodeID][]core.NodeWithActivation{
			"A": {{Node: nodeC, Activation: 0.5}},
		},
	}

	tool := New(sub)
	req := core.ToolRequest{
		ProjectID: "test",
		IR:        core.IR{},
		Anchors: []core.Anchor{
			{Node: &nodeA, Edges: nil},
		},
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
		t.Errorf("expected emission to contain 'FuncA', got: %s", content)
	}
	if !containsStr(content, "FuncB") {
		t.Errorf("expected emission to contain caller 'FuncB'")
	}
	if !containsStr(content, "FuncC") {
		t.Errorf("expected emission to contain callee 'FuncC'")
	}
}

// TestExecute_ProposesSpeculativeEdge verifies speculative edge proposal for new callees.
func TestExecute_ProposesSpeculativeEdge(t *testing.T) {
	nodeA := makeSymbolNode("A", "FuncA")
	nodeC := makeSymbolNode("C", "FuncC")

	sub := &mockSubstrate{
		callees: map[core.NodeID][]core.NodeWithActivation{
			"A": {{Node: nodeC, Activation: 0.5}},
		},
	}

	tool := New(sub)
	req := core.ToolRequest{
		ProjectID: "test",
		Anchors:   []core.Anchor{{Node: &nodeA, Edges: nil}},
	}

	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(result.ProposedEdges) == 0 {
		t.Fatal("expected a proposed edge for undiscovered callee")
	}
	if result.ProposedEdges[0].SourceClass != core.SourceSpeculative {
		t.Errorf("expected speculative source class, got %s", result.ProposedEdges[0].SourceClass)
	}
}

// TestExecute_SkipsNonSymbols verifies non-symbol anchors are skipped.
func TestExecute_SkipsNonSymbols(t *testing.T) {
	nsNode := core.Node{ID: "NS", Type: core.NodeTypeNamespace, Label: "pkg"}
	tool := New(&mockSubstrate{})
	req := core.ToolRequest{
		ProjectID: "test",
		Anchors:   []core.Anchor{{Node: &nsNode}},
	}
	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(result.Emissions) > 0 {
		t.Error("expected no emissions for namespace anchor")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
