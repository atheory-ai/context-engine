// Package indexer orchestrates the full and incremental index of a project.
// It walks the project directory, routes files to language handlers via loaded
// plugins, extracts nodes and edges, and writes them to the substrate.
//
// Dependency rules (from spec):
//   - imports internal/core
//   - imports internal/graph/substrate (writer)
//   - imports internal/plugins (registry)
//   - NO dependency on internal/agent or internal/runner
package indexer

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/indexer/parser"
	"github.com/atheory/context-engine/internal/indexer/walker"
)

// Config holds the parameters for a single index run.
type Config struct {
	// RootDir is the absolute path to the directory to index.
	RootDir string

	// ProjectID is the project whose graph DB receives the writes.
	// Phase 1: always "local".
	ProjectID core.ProjectID

	// IndexerCfg is the include/exclude/size configuration from ce.yaml.
	IndexerCfg config.IndexerConfig

	// Full requests a complete reindex regardless of previous index state.
	// Phase 1: always treated as full (incremental not yet implemented).
	Full bool
}

// Stats summarises the results of an index run.
type Stats struct {
	FilesWalked  int
	FilesIndexed int
	FilesSkipped int
	FilesErrored int
	NodesWritten int
	EdgesWritten int
	Duration     time.Duration
}

// Indexer orchestrates file extraction and substrate writes for a single project.
type Indexer struct {
	cfg      Config
	plugins  []core.Plugin
	writer   core.SubstrateWriter
	channels *core.AppChannels
}

// New creates a ready-to-run Indexer.
//
// plugins is the list of loaded plugins (from plugins.Registry.Loaded()).
// writer must route writes to the project graph DB identified by cfg.ProjectID —
// the caller is responsible for mounting that DB before calling Run.
func New(cfg Config, plugins []core.Plugin, writer core.SubstrateWriter, channels *core.AppChannels) *Indexer {
	return &Indexer{
		cfg:      cfg,
		plugins:  plugins,
		writer:   writer,
		channels: channels,
	}
}

// Run performs the index and returns statistics.
// Blocks until complete or ctx is cancelled.
// A cancelled context returns ctx.Err() as the error; partial results are valid.
func (idx *Indexer) Run(ctx context.Context) (Stats, error) {
	start := time.Now()
	var stats Stats

	// Build the parser from loaded plugins' language handlers.
	psr := parser.New(idx.plugins)
	if psr.HandlerCount() == 0 {
		idx.emitWarning("no language handlers found in loaded plugins — nothing to index")
		return stats, nil
	}

	// Walk the directory tree.
	idx.emitProgress(fmt.Sprintf("Walking %s...", idx.cfg.RootDir))
	files, walkErr := walker.Walk(ctx, idx.cfg.RootDir, idx.cfg.IndexerCfg)
	stats.FilesWalked = len(files)

	if walkErr != nil && ctx.Err() == nil {
		return stats, fmt.Errorf("walk %s: %w", idx.cfg.RootDir, walkErr)
	}

	idx.emitProgress(fmt.Sprintf("Found %d files, extracting...", len(files)))

	now := time.Now().UnixMilli()

	// Process each file.
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}

		handler, pluginID, ok := psr.Route(f.Path)
		if !ok {
			stats.FilesSkipped++
			continue
		}

		content, err := os.ReadFile(f.Path)
		if err != nil {
			stats.FilesErrored++
			idx.emitWarning(fmt.Sprintf("read %s: %v", f.RelPath, err))
			continue
		}

		result, err := handler.Extract(f.RelPath, content)
		if err != nil {
			stats.FilesErrored++
			idx.emitWarning(fmt.Sprintf("extract %s: %v", f.RelPath, err))
			continue
		}

		if len(result.Nodes) == 0 && len(result.Edges) == 0 {
			stats.FilesSkipped++
			continue
		}

		// Remap IDs from the plugin's empty projectID to the real projectID.
		remapped := remapIDs(result, idx.cfg.ProjectID, pluginID, now)

		// Write nodes to the substrate via the write buffer.
		for _, node := range remapped.Nodes {
			if err := idx.writer.UpsertNode(ctx, node); err != nil {
				idx.emitWarning(fmt.Sprintf("write node %s: %v", node.CanonicalID, err))
				continue
			}
			stats.NodesWritten++
		}

		// Write edges.
		for _, edge := range remapped.Edges {
			if err := idx.writer.UpsertEdge(ctx, edge); err != nil {
				idx.emitWarning(fmt.Sprintf("write edge %s: %v", string(edge.ID), err))
				continue
			}
			stats.EdgesWritten++
		}

		// Run analyzer passes — each produces additional edges from extracted nodes.
		for _, p := range idx.plugins {
			for _, analyzer := range p.Analyzers() {
				extraEdges, err := analyzer.Analyze(remapped.Nodes)
				if err != nil {
					idx.emitWarning(fmt.Sprintf("analyzer %s on %s: %v",
						analyzer.Name(), f.RelPath, err))
					continue
				}
				for _, edge := range extraEdges {
					if err := idx.writer.UpsertEdge(ctx, edge); err != nil {
						idx.emitWarning(fmt.Sprintf("write analyzer edge: %v", err))
						continue
					}
					stats.EdgesWritten++
				}
			}
		}

		stats.FilesIndexed++
	}

	stats.Duration = time.Since(start)
	idx.emitProgress(fmt.Sprintf(
		"Indexed %d/%d files — %d nodes, %d edges (%s)",
		stats.FilesIndexed, stats.FilesWalked,
		stats.NodesWritten, stats.EdgesWritten,
		stats.Duration.Round(time.Millisecond),
	))

	if ctx.Err() != nil {
		return stats, ctx.Err()
	}
	return stats, nil
}

// remapIDs re-generates node and edge IDs using the real projectID.
// Plugins generate IDs with an empty projectID (""); the engine uses the real one.
// The remapping ensures the substrate's deterministic ID contract is maintained.
func remapIDs(
	result core.ExtractionResult,
	projectID core.ProjectID,
	pluginID core.PluginID,
	now int64,
) core.ExtractionResult {
	pidStr := string(projectID)

	// Remap nodes and build an oldID → newID translation table for edges.
	oldToNew := make(map[core.NodeID]core.NodeID, len(result.Nodes))
	nodes := make([]core.Node, len(result.Nodes))

	for i, n := range result.Nodes {
		newID := core.NodeID(core.MakeNodeID(pidStr, n.Type, n.CanonicalID))
		oldToNew[n.ID] = newID

		sc := n.SourceClass
		if sc == "" {
			sc = core.SourceStructural
		}
		props := n.Properties
		if props == nil {
			props = map[string]any{}
		}
		nodes[i] = core.Node{
			ID:          newID,
			ProjectID:   projectID,
			Type:        n.Type,
			Label:       n.Label,
			CanonicalID: n.CanonicalID,
			SourceClass: sc,
			PluginID:    pluginID,
			Properties:  props,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}

	// Remap edges, translating source/target IDs through the translation table.
	edges := make([]core.Edge, len(result.Edges))
	for i, e := range result.Edges {
		sourceID, ok := oldToNew[e.SourceID]
		if !ok {
			sourceID = e.SourceID // cross-extraction reference — keep as-is
		}
		targetID, ok2 := oldToNew[e.TargetID]
		if !ok2 {
			targetID = e.TargetID
		}
		newID := core.EdgeID(core.MakeEdgeID(string(sourceID), e.Type, string(targetID)))

		sc := e.SourceClass
		if sc == "" {
			sc = core.SourceStructural
		}
		props := e.Properties
		if props == nil {
			props = map[string]any{}
		}
		edges[i] = core.Edge{
			ID:          newID,
			ProjectID:   projectID,
			SourceID:    sourceID,
			TargetID:    targetID,
			Type:        e.Type,
			SourceClass: sc,
			PluginID:    pluginID,
			Properties:  props,
			CreatedAt:   now,
		}
	}

	return core.ExtractionResult{Nodes: nodes, Edges: edges}
}

func (idx *Indexer) emitProgress(msg string) {
	idx.channels.Emit(core.Emission{
		Source:  "indexer",
		Channel: core.ChanProgress,
		Content: msg,
	})
}

func (idx *Indexer) emitWarning(msg string) {
	idx.channels.Emit(core.Emission{
		Source:  "indexer",
		Channel: core.ChanWarning,
		Content: msg,
	})
}
