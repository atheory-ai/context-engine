package runner

import (
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/graph/substrate"
	"github.com/atheory-ai/context-engine/internal/plugins"
	"github.com/atheory-ai/context-engine/internal/tools/callgraph"
	"github.com/atheory-ai/context-engine/internal/tools/concepts"
	"github.com/atheory-ai/context-engine/internal/tools/crossproject"
	"github.com/atheory-ai/context-engine/internal/tools/filecontext"
	"github.com/atheory-ai/context-engine/internal/tools/references"
	"github.com/atheory-ai/context-engine/internal/tools/summary"
)

// buildToolList assembles all available tools: built-in + plugin-contributed.
// Called once per Engine instantiation; the list is shared across all queries.
func buildToolList(sub *substrate.ReadWriter, reg *plugins.Registry) []core.Tool {
	var tools []core.Tool

	// Built-in tools — always available.
	tools = append(tools,
		callgraph.New(sub),
		references.New(sub),
		crossproject.New(sub),
		concepts.New(sub),
		filecontext.New(sub),
		summary.New(sub),
	)

	// Plugin-contributed tools — added at load time.
	for _, plugin := range reg.Loaded() {
		tools = append(tools, plugin.Tools()...)
	}

	return tools
}
