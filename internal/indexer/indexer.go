// Package indexer orchestrates full and incremental indexing of a project.
// It walks the directory tree, routes files to language plugins, parses
// them with tree-sitter, calls plugin extract(), and writes results to
// the substrate write buffer.
//
// Dependency rules:
//   - imports internal/core, internal/config
//   - imports internal/indexer/wasmparse, internal/indexer/walker
//   - imports internal/plugins (registry)
//   - imports internal/storage/queries (IndexQueries)
//   - NO dependency on internal/agent or internal/runner
package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/indexer/walker"
	"github.com/atheory-ai/context-engine/internal/indexer/wasmparse"
	"github.com/atheory-ai/context-engine/internal/plugins"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

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

// Indexer orchestrates full and incremental indexing for a project.
type Indexer struct {
	cfg       *config.Config
	plugins   *plugins.Registry
	wasm      *wasmparse.Parser // pure-Go tree-sitter (WASM on wazero, no CGO)
	substrate core.SubstrateWriter
	queries   *queries.IndexQueries
	channels  *core.AppChannels
}

// New creates an Indexer backed by the given plugin registry. Files are parsed
// by the pure-Go WASM tree-sitter (wazero); a failure to init it is non-fatal
// but disables structural extraction (plugins receive tree: null).
func New(
	cfg *config.Config,
	pluginReg *plugins.Registry,
	substrate core.SubstrateWriter,
	indexQueries *queries.IndexQueries,
	channels *core.AppChannels,
) *Indexer {
	// Persist the WASM compilation cache to disk so grammars/core compile once
	// and are reused across `ce index` runs. Falls back to an in-memory cache
	// when no data dir is configured.
	wasmCacheDir := ""
	if cfg.DataDir != "" {
		wasmCacheDir = filepath.Join(cfg.DataDir, "cache", "wazero-parse")
	}
	wasm, err := wasmparse.New(context.Background(), wasmCacheDir)
	if err != nil {
		channels.Emit(core.Emission{
			Source:  "indexer",
			Channel: core.ChanWarning,
			Content: fmt.Sprintf("wasm parser init: %v", err),
		})
		wasm = nil
	}

	// Register grammars declared by loaded plugins, so a plugin can add a
	// language at runtime without an engine rebuild. Failures are warnings —
	// the language just goes unparsed.
	if wasm != nil {
		for _, pl := range pluginReg.Loaded() {
			h := pl.Language()
			if h == nil || h.GrammarPath() == "" {
				continue
			}
			wb, err := os.ReadFile(h.GrammarPath())
			if err == nil {
				_, err = wasm.RegisterGrammar(h.Extensions(), wb)
			}
			if err != nil {
				channels.Emit(core.Emission{
					Source:  "indexer",
					Channel: core.ChanWarning,
					Content: fmt.Sprintf("plugin %s grammar: %v", pl.ID(), err),
				})
			}
		}
	}

	return &Indexer{
		cfg:       cfg,
		plugins:   pluginReg,
		wasm:      wasm,
		substrate: substrate,
		queries:   indexQueries,
		channels:  channels,
	}
}

// parseFile returns the serialized SyntaxTree JSON for the plugin boundary, or
// nil if no bundled grammar handles the file (plugin receives tree: null).
func (idx *Indexer) parseFile(ctx context.Context, relPath string, content []byte) ([]byte, error) {
	if idx.wasm == nil {
		return nil, nil
	}
	return idx.wasm.ParseFile(ctx, relPath, content)
}

// reindexTracker records, across the concurrent workers, which files were seen
// this run and the fresh node ids each changed file produced — the inputs to
// the post-pass incremental prune.
type reindexTracker struct {
	mu      sync.Mutex
	walked  map[string]struct{}
	changed map[string][]string // relPath → fresh node ids, only for files that had a prior hash
}

func (t *reindexTracker) markWalked(relPath string) {
	t.mu.Lock()
	t.walked[relPath] = struct{}{}
	t.mu.Unlock()
}

func (t *reindexTracker) markChanged(relPath string, ids []string) {
	t.mu.Lock()
	t.changed[relPath] = ids
	t.mu.Unlock()
}

// Run performs a full or incremental index of rootDir.
// projectID identifies the substrate graph to write to.
// full=true forces reindex of all files regardless of cached hashes.
// Blocks until complete or ctx is cancelled.
func (idx *Indexer) Run(ctx context.Context, rootDir string, projectID core.ProjectID, full bool) (Stats, error) {
	start := time.Now()

	idx.emitProgress(fmt.Sprintf("indexing %s (mode: %s)",
		projectID, map[bool]string{true: "full", false: "incremental"}[full]))

	// Load existing file hashes for incremental mode.
	var existingHashes map[string]string
	if !full && idx.queries != nil {
		var err error
		existingHashes, err = idx.queries.GetFileHashes(ctx, string(projectID))
		if err != nil {
			return Stats{}, fmt.Errorf("load file hashes: %w", err)
		}
	}

	// For a full reindex, clear the stored hashes so removed files are cleaned up.
	if full && idx.queries != nil {
		if err := idx.queries.ClearFileHashes(ctx, string(projectID)); err != nil {
			idx.emitWarning(fmt.Sprintf("clear file hashes: %v", err))
		}
	}

	// Set up the directory walker.
	w, err := walker.New(rootDir, walker.Config{
		ExcludePatterns:  idx.cfg.Indexer.Exclude,
		MaxFileSizeBytes: idx.cfg.Indexer.MaxFileSizeBytes,
	})
	if err != nil {
		return Stats{}, fmt.Errorf("create walker: %w", err)
	}

	fileResults := make(chan walker.WalkResult, 64)
	walkErrCh := make(chan error, 1)
	go func() {
		walkErrCh <- w.Walk(ctx, fileResults)
	}()

	// Process files concurrently — cap at 8 workers.
	workerCount := runtime.NumCPU()
	if workerCount > 8 {
		workerCount = 8
	}

	var (
		wg           sync.WaitGroup
		filesWalked  int64
		filesIndexed int64
		filesSkipped int64
		filesErrored int64
		nodesWritten int64
		edgesWritten int64
	)

	now := time.Now().UnixMilli()

	tracker := &reindexTracker{walked: map[string]struct{}{}, changed: map[string][]string{}}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for result := range fileResults {
				atomic.AddInt64(&filesWalked, 1)
				tracker.markWalked(result.RelPath)
				n, e, err := idx.processFile(ctx, projectID, result, existingHashes, now, tracker)
				if err != nil {
					atomic.AddInt64(&filesErrored, 1)
					idx.emitWarning(fmt.Sprintf("index %s: %v", result.RelPath, err))
					continue
				}
				if n < 0 {
					// Skipped: unchanged hash or no matching plugin.
					atomic.AddInt64(&filesSkipped, 1)
					continue
				}
				atomic.AddInt64(&filesIndexed, 1)
				atomic.AddInt64(&nodesWritten, int64(n))
				atomic.AddInt64(&edgesWritten, int64(e))
			}
		}()
	}

	wg.Wait()

	if walkErr := <-walkErrCh; walkErr != nil && ctx.Err() == nil {
		return Stats{}, fmt.Errorf("walk: %w", walkErr)
	}

	// Flush the write buffer so all writes are committed before pruning.
	if err := idx.substrate.Flush(ctx); err != nil {
		idx.emitWarning(fmt.Sprintf("flush write buffer: %v", err))
	}

	// Incremental prune: now that the fresh nodes are committed, drop the stale
	// ones. For a changed file, remove the symbols it no longer produces; for a
	// file gone from disk, remove everything it contributed. (A full reindex
	// re-stamps every node's source_file, which is what makes this exact.)
	if !full && idx.queries != nil && ctx.Err() == nil {
		idx.pruneStale(ctx, projectID, existingHashes, tracker)
	}

	stats := Stats{
		FilesWalked:  int(filesWalked),
		FilesIndexed: int(filesIndexed),
		FilesSkipped: int(filesSkipped),
		FilesErrored: int(filesErrored),
		NodesWritten: int(nodesWritten),
		EdgesWritten: int(edgesWritten),
		Duration:     time.Since(start),
	}

	idx.emitProgress(fmt.Sprintf(
		"index complete: %d files, %d nodes, %d edges (%s)",
		stats.FilesIndexed, stats.NodesWritten, stats.EdgesWritten,
		stats.Duration.Round(time.Millisecond),
	))

	if ctx.Err() != nil {
		return stats, ctx.Err()
	}
	return stats, nil
}

// processFile processes a single file and writes nodes/edges to the substrate.
// Returns (nodesWritten, edgesWritten, nil) on success.
// Returns (-1, 0, nil) if the file should be skipped (unchanged or no plugin).
// Returns (0, 0, err) on a processing error.
func (idx *Indexer) processFile(
	ctx context.Context,
	projectID core.ProjectID,
	result walker.WalkResult,
	existingHashes map[string]string,
	now int64,
	tracker *reindexTracker,
) (int, int, error) {
	content, err := os.ReadFile(result.Path)
	if err != nil {
		return 0, 0, fmt.Errorf("read: %w", err)
	}

	hash := fileHash(content)

	// A file with a prior hash that we're re-processing is a change — its stale
	// symbols get pruned after the run (a brand-new file has nothing to prune).
	_, changed := existingHashes[result.RelPath]

	// Incremental check — skip if content unchanged.
	if existingHashes != nil {
		if existingHash, ok := existingHashes[result.RelPath]; ok && existingHash == hash {
			return -1, 0, nil
		}
	}

	// Find all plugins that handle this file. Generic language plugins provide
	// structural symbols; convention plugins can add framework-specific meaning.
	matchingPlugins := idx.plugins.PluginsForFile(result.RelPath)
	if len(matchingPlugins) == 0 {
		return -1, 0, nil // no plugin handles this extension
	}

	// Parse the file to the serialized SyntaxTree the plugin consumes.
	// treeJSON is nil if no bundled grammar handles the file.
	treeJSON, err := idx.parseFile(ctx, result.RelPath, content)
	if err != nil {
		return 0, 0, fmt.Errorf("parse: %w", err)
	}

	nodesOut := 0
	edgesOut := 0
	successfulPlugins := 0
	extractErrors := 0
	var filePluginIIR []core.IIRExtracted // plugin-lifted IIR (Track B), if any
	var keepIDs []string                  // ids emitted this run — survivors of the prune
	for _, p := range matchingPlugins {
		langHandler := p.Language()
		if langHandler == nil {
			continue
		}

		// Call the plugin's extract() function.
		extraction, err := langHandler.Extract(result.RelPath, content, treeJSON)
		if err != nil {
			idx.emitWarning(fmt.Sprintf("extract %s with %s: %v", result.RelPath, p.ID(), err))
			extractErrors++
			continue
		}
		successfulPlugins++

		// Remap node/edge IDs from the plugin's empty project context to the real projectID.
		remapped := remapIDs(extraction, projectID, p.ID(), now)
		filePluginIIR = append(filePluginIIR, remapped.IIR...)

		for _, node := range remapped.Nodes {
			node.SourceFile = result.RelPath // stamp for incremental pruning
			if err := idx.substrate.UpsertNode(ctx, node); err != nil {
				idx.emitWarning(fmt.Sprintf("write node %s: %v", node.CanonicalID, err))
				continue
			}
			keepIDs = append(keepIDs, string(node.ID))
			nodesOut++
		}

		for _, edge := range remapped.Edges {
			if err := idx.substrate.UpsertEdge(ctx, edge); err != nil {
				idx.emitWarning(fmt.Sprintf("write edge: %v", err))
				continue
			}
			edgesOut++
		}

		// Run analyzer passes — each produces additional edges from extracted nodes.
		for _, analyzer := range p.Analyzers() {
			extraEdges, err := analyzer.Analyze(remapped.Nodes)
			if err != nil {
				idx.emitWarning(fmt.Sprintf("analyzer %s on %s: %v", analyzer.Name(), result.RelPath, err))
				continue
			}
			for _, edge := range extraEdges {
				edge.ProjectID = projectID
				if err := idx.substrate.UpsertEdge(ctx, edge); err != nil {
					idx.emitWarning(fmt.Sprintf("write analyzer edge: %v", err))
					continue
				}
				edgesOut++
			}
		}
	}

	if successfulPlugins == 0 && extractErrors > 0 {
		return 0, 0, fmt.Errorf("extract failed for all matching plugins")
	}

	// Store the IIR the language plugin lifted and attached to its nodes. The
	// host no longer runs its own extractor at index time — IIR is owned entirely
	// by plugins (Track B); files no plugin lifts simply get no IIR.
	if idx.cfg.IIR.Enabled && len(filePluginIIR) > 0 {
		idx.writePluginIIR(ctx, projectID, hash, filePluginIIR, now)
	}

	// Persist the file hash for future incremental runs.
	if idx.queries != nil {
		_ = idx.queries.UpsertFileHash(ctx, string(projectID), result.RelPath, hash) //nolint:errcheck // best-effort; next run re-hashes if missing
	}

	// A changed file may have dropped symbols since last index; record the ids it
	// still emits so the post-run prune can remove the rest.
	if changed && tracker != nil {
		tracker.markChanged(result.RelPath, keepIDs)
	}

	return nodesOut, edgesOut, nil
}

// pruneStale removes nodes left over from a previous index that the current
// incremental run supersedes: symbols dropped from changed files, and every
// node contributed by files that no longer exist on disk. Runs after the write
// buffer is flushed, single-threaded, so its direct deletes don't contend with
// buffered writes.
func (idx *Indexer) pruneStale(ctx context.Context, projectID core.ProjectID, existingHashes map[string]string, tracker *reindexTracker) {
	// Changed files: drop the symbols they no longer produce.
	for relPath, keepIDs := range tracker.changed {
		if _, err := idx.queries.PruneFileNodes(ctx, string(projectID), relPath, keepIDs); err != nil {
			idx.emitWarning(fmt.Sprintf("prune changed %s: %v", relPath, err))
		}
	}
	// Deleted files: present in the prior index, absent from this walk.
	for relPath := range existingHashes {
		if _, seen := tracker.walked[relPath]; seen {
			continue
		}
		if _, err := idx.queries.PruneFileNodes(ctx, string(projectID), relPath, nil); err != nil {
			idx.emitWarning(fmt.Sprintf("prune deleted %s: %v", relPath, err))
		}
		if err := idx.queries.DeleteFileHash(ctx, string(projectID), relPath); err != nil {
			idx.emitWarning(fmt.Sprintf("delete hash %s: %v", relPath, err))
		}
	}
}

// fileHash returns the SHA-256 hash of content as a lowercase hex string.
func fileHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// remapIDs re-generates node and edge IDs using the real projectID.
// Plugins produce IDs with an empty projectID; the engine uses the real one.
func remapIDs(
	result core.ExtractionResult,
	projectID core.ProjectID,
	pluginID core.PluginID,
	now int64,
) core.ExtractionResult {
	pidStr := string(projectID)

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

	// Remap plugin-lifted IIR onto the real node ids (the plugin attached each
	// intent to a node it created under the empty-project context).
	var iirOut []core.IIRExtracted
	if len(result.IIR) > 0 {
		iirOut = make([]core.IIRExtracted, 0, len(result.IIR))
		for _, e := range result.IIR {
			nodeID, ok := oldToNew[e.NodeID]
			if !ok {
				nodeID = e.NodeID // reference outside this extraction — keep as-is
			}
			iirOut = append(iirOut, core.IIRExtracted{NodeID: nodeID, Intent: e.Intent})
		}
	}

	return core.ExtractionResult{Nodes: nodes, Edges: edges, IIR: iirOut}
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
