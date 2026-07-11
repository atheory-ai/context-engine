package summary

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/tools/shared"
)

func newTool() *Tool { return New(shared.NilSubstrate{}) }

func TestMetadata(t *testing.T) {
	tool := newTool()
	if tool.Name() != "summary" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Description() == "" || tool.ActivationHint() == "" {
		t.Error("Description/ActivationHint should be non-empty")
	}
}

func TestActivate(t *testing.T) {
	tool := newTool()
	if !tool.Activate(core.IR{Anchors: []core.AnchorRef{{Type: "namespace"}}}) {
		t.Error("a namespace anchor should activate the tool")
	}
	if !tool.Activate(core.IR{Predicates: map[string]string{"summary": "true"}}) {
		t.Error("the summary predicate should activate the tool")
	}
	if tool.Activate(core.IR{Anchors: []core.AnchorRef{{Type: "symbol"}}}) {
		t.Error("a non-namespace anchor alone should not activate")
	}
	if tool.Activate(core.IR{}) {
		t.Error("an empty IR should not activate")
	}
}

func TestExecute_NoNamespaceAnchors(t *testing.T) {
	res, err := newTool().Execute(context.Background(), core.ToolRequest{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.ProposedNodes) != 0 {
		t.Errorf("expected no proposed nodes, got %+v", res)
	}
}
