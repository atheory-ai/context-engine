package filecontext

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/tools/shared"
)

func newTool() *Tool { return New(shared.NilSubstrate{}) }

func TestMetadata(t *testing.T) {
	tool := newTool()
	if tool.Name() != "filecontext" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Description() == "" || tool.ActivationHint() == "" {
		t.Error("Description/ActivationHint should be non-empty")
	}
}

func TestActivate(t *testing.T) {
	tool := newTool()

	if !tool.Activate(core.IR{Anchors: []core.AnchorRef{{Type: "file", ID: "x"}}}) {
		t.Error("a file anchor should activate the tool")
	}
	if !tool.Activate(core.IR{Predicates: map[string]string{"filecontext": "true"}}) {
		t.Error("the filecontext predicate should activate the tool")
	}
	if tool.Activate(core.IR{Anchors: []core.AnchorRef{{Type: "symbol"}}}) {
		t.Error("a non-file anchor alone should not activate")
	}
	if tool.Activate(core.IR{}) {
		t.Error("an empty IR should not activate")
	}
}

func TestExecute_NoFileAnchors(t *testing.T) {
	// With no file anchors, Execute never touches the substrate and returns an
	// empty result without error.
	res, err := newTool().Execute(context.Background(), core.ToolRequest{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.ProposedNodes) != 0 || len(res.ProposedEdges) != 0 {
		t.Errorf("expected no proposed changes, got %+v", res)
	}
}
