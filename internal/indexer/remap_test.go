package indexer

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

func TestRemapIDsResolvesSharedFileReferenceFromConventionPlugin(t *testing.T) {
	projectID := core.ProjectID("project")
	fileCanonicalID := "plugins/demo.php"
	pluginFileID := core.NodeID(core.MakeNodeID("", "file", fileCanonicalID))
	fileID := core.NodeID(core.MakeNodeID(string(projectID), "file", fileCanonicalID))

	// The generic PHP extraction owns the structural file node.
	references := map[core.NodeID]core.NodeID{pluginFileID: fileID}
	// The convention extraction deliberately does not duplicate that node, but
	// emits a framework fact attached to it.
	fact := core.Node{ID: core.NodeID("fact"), Type: "wordpress_hook", CanonicalID: "wordpress:hook:demo"}
	result := core.ExtractionResult{
		Nodes: []core.Node{fact},
		Edges: []core.Edge{{ID: "edge", SourceID: pluginFileID, TargetID: fact.ID, Type: "contains"}},
	}

	remapped := remapIDsWithReferences(result, projectID, "com.example.wordpress", 1, references)
	if got := remapped.Edges[0].SourceID; got != fileID {
		t.Errorf("edge source = %q, want shared file ID %q", got, fileID)
	}
	if got := remapped.Edges[0].TargetID; got != remapped.Nodes[0].ID {
		t.Errorf("edge target = %q, want remapped convention node %q", got, remapped.Nodes[0].ID)
	}
}
