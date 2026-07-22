package indexer

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

func TestMaterializeUnresolvedEndpointsAddsOnlyMissingAnchors(t *testing.T) {
	project := core.ProjectID("project")
	nodes := []core.Node{{ID: "present", ProjectID: project, Type: core.NodeTypeSymbol}}
	edges := []core.Edge{{ID: "edge", ProjectID: project, SourceID: "present", TargetID: "external", Type: "extends", SourceClass: core.SourceSpeculative, PluginID: "plugin"}}

	got := materializeUnresolvedEndpoints(nodes, edges, project, "src/example.ts", "run", 1)
	if len(got) != 2 {
		t.Fatalf("nodes = %d, want 2", len(got))
	}
	anchor := got[1]
	if anchor.ID != "external" || anchor.Type != "unresolved" || anchor.CanonicalID != "unresolved:external" {
		t.Fatalf("unresolved anchor = %#v", anchor)
	}
	if anchor.SourceClass != core.SourceSpeculative || anchor.SourceFile != "src/example.ts" || !anchor.IndexManaged {
		t.Fatalf("unresolved anchor provenance = %#v", anchor)
	}

	got = materializeUnresolvedEndpoints(got, edges, project, "src/example.ts", "run", 1)
	if len(got) != 2 {
		t.Fatalf("materializing twice produced %d nodes, want 2", len(got))
	}
}
