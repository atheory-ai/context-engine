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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	FilesWalked                 int
	FilesIndexed                int
	FilesSkipped                int
	FilesErrored                int
	NodesWritten                int
	EdgesWritten                int
	SourceBytesProcessed        int64
	CSTBytesProcessed           int64
	PluginPayloadBytesEstimated int64
	Duration                    time.Duration
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

type indexTransactionWriter interface {
	BeginIndexTransaction(context.Context) error
	CommitIndexTransaction(context.Context) error
	AbortIndexTransaction(context.Context) error
}

type preparedFile struct {
	result   walker.WalkResult
	content  []byte
	hash     string
	treeJSON []byte
	plugins  []core.Plugin
	release  func()
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

// Run performs a full or incremental index of rootDir.
// projectID identifies the substrate graph to write to.
// full=true forces reindex of all files regardless of cached hashes.
// Blocks until complete or ctx is cancelled.
func (idx *Indexer) Run(ctx context.Context, rootDir string, projectID core.ProjectID, full bool) (Stats, error) {
	start := time.Now()
	runID, err := newIndexRunID()
	if err != nil {
		return Stats{}, fmt.Errorf("create index run id: %w", err)
	}

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
	if idx.queries != nil {
		pluginIDs := make([]string, 0, len(idx.plugins.Loaded()))
		for _, plugin := range idx.plugins.Loaded() {
			pluginIDs = append(pluginIDs, string(plugin.ID()))
		}
		if err := idx.queries.StartIndexRun(ctx, runID, string(projectID), pluginIDs, start.UnixMilli()); err != nil {
			return Stats{}, err
		}
	}
	transaction, atomicCommit := idx.substrate.(indexTransactionWriter)
	transactionOpen := false
	if atomicCommit {
		if err := transaction.BeginIndexTransaction(ctx); err != nil {
			idx.failRun(ctx, runID, err)
			return Stats{}, fmt.Errorf("begin atomic index write: %w", err)
		}
		transactionOpen = true
		defer func() {
			if transactionOpen {
				if err := transaction.AbortIndexTransaction(context.Background()); err != nil {
					idx.emitWarning(fmt.Sprintf("abort atomic index write: %v", err))
				}
			}
		}()
	}

	// Set up the directory walker.
	w, err := walker.New(rootDir, walker.Config{
		ExcludePatterns:  idx.cfg.Indexer.Exclude,
		MaxFileSizeBytes: idx.cfg.Indexer.MaxFileSizeBytes,
	})
	if err != nil {
		idx.failRun(ctx, runID, err)
		return Stats{}, fmt.Errorf("create walker: %w", err)
	}

	fileResults := make(chan walker.WalkResult, 64)
	walkErrCh := make(chan error, 1)
	go func() {
		walkErrCh <- w.Walk(ctx, fileResults)
	}()

	workerCount := idx.cfg.Indexer.ExtractWorkers
	if workerCount < 1 {
		workerCount = runtime.NumCPU()
		if workerCount > 8 {
			workerCount = 8
		}
	}
	limiter := newByteLimiter(idx.cfg.Indexer.MaxInFlightBytes)

	var (
		wg                 sync.WaitGroup
		filesWalked        int64
		filesIndexed       int64
		filesSkipped       int64
		filesErrored       int64
		nodesWritten       int64
		edgesWritten       int64
		sourceBytes        int64
		cstBytes           int64
		pluginPayloadBytes int64
	)

	now := time.Now().UnixMilli()

	parseWorkers := idx.cfg.Indexer.ParseWorkers
	if parseWorkers < 1 {
		parseWorkers = workerCount
	}
	preparedCh := make(chan *preparedFile, workerCount)
	for i := 0; i < parseWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for result := range fileResults {
				atomic.AddInt64(&filesWalked, 1)
				if idx.queries != nil {
					if err := idx.queries.StageWalked(ctx, runID, string(projectID), result.RelPath); err != nil {
						atomic.AddInt64(&filesErrored, 1)
						continue
					}
				}
				release, err := limiter.acquire(ctx, int(result.Info.Size())*8)
				if err != nil {
					atomic.AddInt64(&filesErrored, 1)
					continue
				}
				prepared, skipped, err := idx.prepareFile(ctx, projectID, result, existingHashes, now)
				if skipped {
					release()
					atomic.AddInt64(&filesSkipped, 1)
					continue
				}
				if err != nil {
					release()
					atomic.AddInt64(&filesErrored, 1)
					idx.emitWarning(fmt.Sprintf("index %s: %v", result.RelPath, err))
					continue
				}
				prepared.release = release
				select {
				case preparedCh <- prepared:
				case <-ctx.Done():
					release()
					atomic.AddInt64(&filesErrored, 1)
				}
			}
		}()
	}

	go func() { wg.Wait(); close(preparedCh) }()
	var extractWG sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		extractWG.Add(1)
		go func() {
			defer extractWG.Done()
			for prepared := range preparedCh {
				n, e, err := idx.processFile(ctx, projectID, prepared, now, runID)
				prepared.release()
				if err != nil {
					atomic.AddInt64(&filesErrored, 1)
					idx.emitWarning(fmt.Sprintf("index %s: %v", prepared.result.RelPath, err))
					continue
				}
				atomic.AddInt64(&filesIndexed, 1)
				atomic.AddInt64(&nodesWritten, int64(n))
				atomic.AddInt64(&edgesWritten, int64(e))
				atomic.AddInt64(&sourceBytes, int64(len(prepared.content)))
				atomic.AddInt64(&cstBytes, int64(len(prepared.treeJSON)))
				atomic.AddInt64(&pluginPayloadBytes, int64(len(prepared.plugins)*(len(prepared.content)+len(prepared.treeJSON))))
			}
		}()
	}
	extractWG.Wait()

	stats := func() Stats {
		return Stats{FilesWalked: int(filesWalked), FilesIndexed: int(filesIndexed), FilesSkipped: int(filesSkipped), FilesErrored: int(filesErrored), NodesWritten: int(nodesWritten), EdgesWritten: int(edgesWritten), SourceBytesProcessed: atomic.LoadInt64(&sourceBytes), CSTBytesProcessed: atomic.LoadInt64(&cstBytes), PluginPayloadBytesEstimated: atomic.LoadInt64(&pluginPayloadBytes), Duration: time.Since(start)}
	}
	if walkErr := <-walkErrCh; walkErr != nil && ctx.Err() == nil {
		err := fmt.Errorf("walk: %w", walkErr)
		idx.failRun(ctx, runID, err)
		return stats(), err
	}

	if filesErrored > 0 || ctx.Err() != nil {
		runErr := ctx.Err()
		if runErr == nil {
			runErr = fmt.Errorf("%d files failed; graph changes were not committed as an index run", filesErrored)
		}
		idx.failRun(ctx, runID, runErr)
		return stats(), runErr
	}
	// Make the fully-validated run visible only after every file completed.
	// Legacy writers fall back to Flush; the engine writer holds all index ops
	// and commits them in a single graph transaction.
	if atomicCommit {
		if err := transaction.CommitIndexTransaction(ctx); err != nil {
			runErr := fmt.Errorf("commit atomic index write: %w", err)
			idx.failRun(ctx, runID, runErr)
			return stats(), runErr
		}
		transactionOpen = false
	} else if err := idx.substrate.Flush(ctx); err != nil {
		runErr := fmt.Errorf("flush write buffer: %w", err)
		idx.failRun(ctx, runID, runErr)
		return stats(), runErr
	}

	// This transaction is the authoritative index-run commit marker. It replaces
	// each processed file's prior membership, removes vanished output (including
	// moved-offset facts), then advances hashes last in the same transaction.
	if idx.queries != nil {
		if err := idx.queries.ReconcileStagedIndexRun(ctx, string(projectID), runID, full, int(filesIndexed), int(nodesWritten), int(edgesWritten), time.Now().UnixMilli()); err != nil {
			idx.failRun(ctx, runID, err)
			return stats(), fmt.Errorf("reconcile index run: %w", err)
		}
	}

	finalStats := stats()

	idx.emitProgress(fmt.Sprintf(
		"index complete: %d files, %d nodes, %d edges (%s)",
		finalStats.FilesIndexed, finalStats.NodesWritten, finalStats.EdgesWritten,
		finalStats.Duration.Round(time.Millisecond),
	))

	if ctx.Err() != nil {
		return finalStats, ctx.Err()
	}
	return finalStats, nil
}

// prepareFile performs the reusable, bounded parse stage.
func (idx *Indexer) prepareFile(ctx context.Context, projectID core.ProjectID, result walker.WalkResult, existingHashes map[string]string, now int64) (*preparedFile, bool, error) {
	content, err := os.ReadFile(result.Path)
	if err != nil {
		return nil, false, fmt.Errorf("read: %w", err)
	}
	hash := fileHash(content)
	if existingHashes != nil && existingHashes[result.RelPath] == hash {
		return nil, true, nil
	}
	plugins, err := idx.plugins.IndexPlanForFile(result.RelPath)
	if err != nil {
		return nil, false, fmt.Errorf("resolve plugin composition: %w", err)
	}
	if len(plugins) == 0 {
		return nil, true, nil
	}
	treeJSON, err := idx.parseFile(ctx, result.RelPath, content)
	if err != nil {
		return nil, false, fmt.Errorf("parse: %w", err)
	}
	if idx.queries != nil {
		compact, err := compactCST(treeJSON)
		if err != nil {
			return nil, false, err
		}
		if err := idx.queries.StoreParseArtifact(ctx, string(projectID), hash, "tree-sitter-wasm/v1", filepath.Ext(result.RelPath), content, compact, now); err != nil {
			return nil, false, err
		}
	}
	return &preparedFile{result: result, content: content, hash: hash, treeJSON: treeJSON, plugins: plugins}, false, nil
}

// processFile performs extraction and staged graph writes for a prepared file.
// Returns (nodesWritten, edgesWritten, nil) on success.
// Returns (-1, 0, nil) if the file should be skipped (unchanged or no plugin).
// Returns (0, 0, err) on a processing error.
func (idx *Indexer) processFile(
	ctx context.Context,
	projectID core.ProjectID,
	prepared *preparedFile,
	now int64,
	runID string,
) (int, int, error) {
	result, content, hash, treeJSON, matchingPlugins := prepared.result, prepared.content, prepared.hash, prepared.treeJSON, prepared.plugins

	nodesOut := 0
	edgesOut := 0
	var filePluginIIR []core.IIRExtracted // plugin-lifted IIR (Track B), if any
	var output queries.FileOutput
	output.Hash = hash
	anchor := canonicalFileAnchor(projectID, result.RelPath, now, runID)
	type pluginExtraction struct {
		plugin     core.Plugin
		extraction core.ExtractionResult
	}
	var extractions []pluginExtraction
	for _, layer := range pluginExtractionLayers(matchingPlugins) {
		results := make([]pluginExtraction, len(layer))
		errs := make([]error, len(layer))
		var layerWG sync.WaitGroup
		for i, p := range layer {
			layerWG.Add(1)
			go func(i int, p core.Plugin) {
				defer layerWG.Done()
				lang := p.Language()
				if lang == nil {
					return
				}
				extraction, err := lang.Extract(result.RelPath, content, treeJSON)
				if err != nil {
					errs[i] = err
					return
				}
				results[i] = pluginExtraction{plugin: p, extraction: extraction}
			}(i, p)
		}
		layerWG.Wait()
		for i, err := range errs {
			if err != nil {
				return 0, 0, fmt.Errorf("extract %s with %s: %w", result.RelPath, layer[i].ID(), err)
			}
		}
		for _, extraction := range results {
			if extraction.plugin != nil {
				extractions = append(extractions, extraction)
			}
		}
	}

	// Language and convention plugins may intentionally share a file node while
	// only one emits it. Build a per-file identity map before remapping edges so
	// an additive plugin can reference that established file node.
	sharedIDs := make(map[core.NodeID]core.NodeID)
	for _, item := range extractions {
		for _, node := range item.extraction.Nodes {
			if node.Type == core.NodeTypeFile {
				sharedIDs[node.ID] = anchor.ID
				continue
			}
			sharedIDs[node.ID] = core.NodeID(core.MakeNodeID(string(projectID), node.Type, node.CanonicalID))
		}
	}
	candidateNodes := []core.Node{anchor}
	var candidateEdges []core.Edge
	for _, item := range extractions {
		p := item.plugin
		// Remap node/edge IDs from the plugin's empty project context to the real projectID.
		remapped := remapIDsWithReferences(item.extraction, projectID, p.ID(), now, sharedIDs, &anchor)
		filePluginIIR = append(filePluginIIR, remapped.IIR...)

		for i := range remapped.Nodes {
			node := remapped.Nodes[i]
			node.SourceFile = result.RelPath // stamp for incremental pruning
			node.IndexManaged = true
			node.LastIndexRunID = runID
			candidateNodes = append(candidateNodes, node)
		}

		for i := range remapped.Edges {
			edge := remapped.Edges[i]
			edge.IndexManaged = true
			edge.LastIndexRunID = runID
			candidateEdges = append(candidateEdges, edge)
		}

		// Run analyzer passes — each produces additional edges from extracted nodes.
		for _, analyzer := range p.Analyzers() {
			extraEdges, err := analyzer.Analyze(remapped.Nodes)
			if err != nil {
				return 0, 0, fmt.Errorf("analyzer %s on %s: %w", analyzer.Name(), result.RelPath, err)
			}
			for _, edge := range extraEdges {
				edge.ProjectID = projectID
				edge.PluginID = p.ID()
				if edge.ID == "" {
					edge.ID = core.EdgeID(core.MakeEdgeID(string(edge.SourceID), edge.Type, string(edge.TargetID)))
				}
				edge.IndexManaged = true
				edge.LastIndexRunID = runID
				candidateEdges = append(candidateEdges, edge)
			}
		}
	}
	mergedNodes, err := mergeContributionNodes(candidateNodes)
	if err != nil {
		return 0, 0, fmt.Errorf("merge file contribution: %w", err)
	}
	mergedEdges, err := mergeContributionEdges(candidateEdges)
	if err != nil {
		return 0, 0, fmt.Errorf("merge file contribution: %w", err)
	}
	for _, node := range mergedNodes {
		if err := idx.substrate.UpsertNode(ctx, node); err != nil {
			return 0, 0, fmt.Errorf("write node %s: %w", node.CanonicalID, err)
		}
		output.NodeIDs = append(output.NodeIDs, string(node.ID))
		nodesOut++
	}
	for _, edge := range mergedEdges {
		if err := idx.substrate.UpsertEdge(ctx, edge); err != nil {
			return 0, 0, fmt.Errorf("write edge: %w", err)
		}
		output.EdgeIDs = append(output.EdgeIDs, string(edge.ID))
		edgesOut++
	}

	// Store the IIR the language plugin lifted and attached to its nodes. The
	// host no longer runs its own extractor at index time — IIR is owned entirely
	// by plugins (Track B); files no plugin lifts simply get no IIR.
	if idx.cfg.IIR.Enabled && len(filePluginIIR) > 0 {
		iirIDs, err := idx.writePluginIIR(ctx, projectID, hash, filePluginIIR, runID, now)
		if err != nil {
			return 0, 0, err
		}
		output.IIRIDs = append(output.IIRIDs, iirIDs...)
	}

	if idx.queries != nil {
		if err := idx.queries.StageFileOutput(ctx, runID, string(projectID), result.RelPath, output); err != nil {
			return 0, 0, fmt.Errorf("stage file output: %w", err)
		}
	}

	return nodesOut, edgesOut, nil
}

// fileHash returns the SHA-256 hash of content as a lowercase hex string.
func fileHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// compactCST removes source and duplicated per-node text while preserving the
// node type, fields, offsets, positions, and child topology needed to recover
// text from the content-addressed source artifact later.
func compactCST(treeJSON []byte) ([]byte, error) {
	if len(treeJSON) == 0 {
		return nil, nil
	}
	var tree any
	if err := json.Unmarshal(treeJSON, &tree); err != nil {
		return nil, err
	}
	var compact func(any)
	compact = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			delete(x, "source")
			delete(x, "text")
			for _, child := range x {
				compact(child)
			}
		case []any:
			for _, child := range x {
				compact(child)
			}
		}
	}
	compact(tree)
	return json.Marshal(tree)
}

// pluginExtractionLayers recreates the manifest dependency DAG as execution
// layers. A legacy plugin acts as a serial barrier: registration order was its
// implicit contract, so declared plugins on either side cannot overtake it.
// Explicitly-declared peers without dependencies can safely fan out.
func pluginExtractionLayers(plugins []core.Plugin) [][]core.Plugin {
	providers := map[string]int{}
	contracts := make([]core.PluginIndexContract, len(plugins))
	explicit := make([]bool, len(plugins))
	for i, plugin := range plugins {
		if c, ok := plugin.(core.IndexContractContributor); ok {
			contracts[i] = c.IndexContract()
			explicit[i] = len(contracts[i].Provides)+len(contracts[i].Requires)+len(contracts[i].Enriches) > 0
			for _, capability := range contracts[i].Provides {
				providers[capability] = i
			}
		}
	}
	deps := make([]map[int]struct{}, len(plugins))
	for i := range plugins {
		deps[i] = map[int]struct{}{}
		if !explicit[i] {
			continue
		}
		for _, capability := range contracts[i].Requires {
			if provider, ok := providers[capability]; ok && provider != i {
				deps[i][provider] = struct{}{}
			}
		}
		for _, language := range contracts[i].Enriches {
			if provider, ok := providers["language:"+language]; ok && provider != i {
				deps[i][provider] = struct{}{}
			}
		}
	}
	// Preserve the serial behaviour of legacy plugins even in a mixed plan.
	// Without this, an explicit plugin after a legacy plugin could run before
	// it merely because it has no declared capabilities.
	for i := 1; i < len(plugins); i++ {
		if !explicit[i-1] || !explicit[i] {
			deps[i][i-1] = struct{}{}
		}
	}
	done := make([]bool, len(plugins))
	var layers [][]core.Plugin
	remaining := len(plugins)
	for remaining > 0 {
		var layer []int
		for i := range plugins {
			if !done[i] && len(deps[i]) == 0 {
				layer = append(layer, i)
			}
		}
		if len(layer) == 0 {
			break
		}
		out := make([]core.Plugin, 0, len(layer))
		for _, i := range layer {
			done[i] = true
			out = append(out, plugins[i])
		}
		remaining -= len(layer)
		for _, i := range layer {
			for child := range plugins {
				delete(deps[child], i)
			}
		}
		layers = append(layers, out)
	}
	return layers
}

func newIndexRunID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func canonicalFileAnchor(projectID core.ProjectID, relPath string, now int64, runID string) core.Node {
	return core.Node{ID: core.NodeID(core.MakeNodeID(string(projectID), core.NodeTypeFile, relPath)), ProjectID: projectID, Type: core.NodeTypeFile, Label: filepath.Base(relPath), CanonicalID: relPath, SourceClass: core.SourceStructural, SourceFile: relPath, IndexManaged: true, LastIndexRunID: runID, Properties: map[string]any{"file_path": relPath}, CreatedAt: now, UpdatedAt: now}
}

// mergeContributionNodes/Edges make same-file plugin composition explicit.
// The former pending-map behaviour silently selected the last writer for a
// duplicate ID; that makes plugin registration order part of graph semantics.
// We instead accept only equivalent claims and a non-conflicting property merge.
func mergeContributionNodes(nodes []core.Node) ([]core.Node, error) {
	byID := make(map[core.NodeID]core.Node, len(nodes))
	order := make([]core.NodeID, 0, len(nodes))
	for _, node := range nodes {
		current, exists := byID[node.ID]
		if !exists {
			byID[node.ID] = node
			order = append(order, node.ID)
			continue
		}
		if current.ProjectID != node.ProjectID || current.Type != node.Type || current.CanonicalID != node.CanonicalID || current.Label != node.Label || current.SourceClass != node.SourceClass || (current.PluginID != "" && node.PluginID != "" && current.PluginID != node.PluginID) {
			return nil, fmt.Errorf("conflicting node claims for %s", node.ID)
		}
		if current.PluginID == "" {
			current.PluginID = node.PluginID
		}
		properties, err := mergeContributionProperties(current.Properties, node.Properties)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", node.ID, err)
		}
		current.Properties = properties
		byID[node.ID] = current
	}
	out := make([]core.Node, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, nil
}

func mergeContributionEdges(edges []core.Edge) ([]core.Edge, error) {
	byID := make(map[core.EdgeID]core.Edge, len(edges))
	order := make([]core.EdgeID, 0, len(edges))
	for _, edge := range edges {
		current, exists := byID[edge.ID]
		if !exists {
			byID[edge.ID] = edge
			order = append(order, edge.ID)
			continue
		}
		if current.ProjectID != edge.ProjectID || current.SourceID != edge.SourceID || current.TargetID != edge.TargetID || current.Type != edge.Type || current.SourceClass != edge.SourceClass || (current.PluginID != "" && edge.PluginID != "" && current.PluginID != edge.PluginID) {
			return nil, fmt.Errorf("conflicting edge claims for %s", edge.ID)
		}
		if current.PluginID == "" {
			current.PluginID = edge.PluginID
		}
		properties, err := mergeContributionProperties(current.Properties, edge.Properties)
		if err != nil {
			return nil, fmt.Errorf("edge %s: %w", edge.ID, err)
		}
		current.Properties = properties
		byID[edge.ID] = current
	}
	out := make([]core.Edge, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, nil
}

func mergeContributionProperties(left, right map[string]any) (map[string]any, error) {
	merged := make(map[string]any, len(left)+len(right))
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		if existing, exists := merged[key]; exists {
			a, _ := json.Marshal(existing)
			b, _ := json.Marshal(value)
			if string(a) != string(b) {
				return nil, fmt.Errorf("property %q has conflicting values", key)
			}
		} else {
			merged[key] = value
		}
	}
	return merged, nil
}

func (idx *Indexer) failRun(ctx context.Context, runID string, runErr error) {
	if idx.queries == nil || runID == "" {
		return
	}
	// The caller's context may already be cancelled; use a short independent
	// context so the failed state remains visible and reads stay guarded.
	markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := idx.queries.FailIndexRun(markCtx, runID, time.Now().UnixMilli(), runErr); err != nil {
		idx.emitWarning(fmt.Sprintf("record failed index run: %v", err))
	}
}

// remapIDsWithReferences additionally resolves node IDs emitted by a sibling
// plugin for the same file. Convention plugins use this to attach framework
// facts to the generic language plugin's file node without duplicating it.
func remapIDsWithReferences(
	result core.ExtractionResult,
	projectID core.ProjectID,
	pluginID core.PluginID,
	now int64,
	references map[core.NodeID]core.NodeID,
	anchor *core.Node,
) core.ExtractionResult {
	pidStr := string(projectID)

	oldToNew := make(map[core.NodeID]core.NodeID, len(result.Nodes))
	nodes := make([]core.Node, 0, len(result.Nodes))

	for _, n := range result.Nodes {
		newID := core.NodeID(core.MakeNodeID(pidStr, n.Type, n.CanonicalID))
		if anchor != nil && n.Type == core.NodeTypeFile {
			newID = anchor.ID
		}
		oldToNew[n.ID] = newID
		if anchor != nil && n.Type == core.NodeTypeFile {
			continue // CE owns the sole canonical file anchor.
		}

		sc := n.SourceClass
		if sc == "" {
			sc = core.SourceStructural
		}
		props := n.Properties
		if props == nil {
			props = map[string]any{}
		}
		nodes = append(nodes, core.Node{
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
		})
	}

	edges := make([]core.Edge, len(result.Edges))
	for i, e := range result.Edges {
		sourceID, ok := oldToNew[e.SourceID]
		if !ok {
			sourceID = e.SourceID
			if mapped, found := references[e.SourceID]; found {
				sourceID = mapped
			}
		}
		targetID, ok2 := oldToNew[e.TargetID]
		if !ok2 {
			targetID = e.TargetID
			if mapped, found := references[e.TargetID]; found {
				targetID = mapped
			}
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
				nodeID = e.NodeID
				if mapped, found := references[e.NodeID]; found {
					nodeID = mapped
				}
			}
			iirOut = append(iirOut, core.IIRExtracted{NodeID: nodeID, SchemaVersion: e.SchemaVersion, Coverage: e.Coverage, Intent: e.Intent, Claims: e.Claims, Evidence: e.Evidence})
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
