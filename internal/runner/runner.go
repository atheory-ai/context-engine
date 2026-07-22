// Package runner is the cognitive loop engine.
// It wires the static DAG, executes the loop, manages concurrency,
// tracks the token budget, and handles all exit conditions.
//
// The entry point is New() → Engine. Callers call Query() to run a query.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/graph/substrate"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/indexer"
	"github.com/atheory-ai/context-engine/internal/llm"
	"github.com/atheory-ai/context-engine/internal/llm/anthropic"
	"github.com/atheory-ai/context-engine/internal/llm/local"
	"github.com/atheory-ai/context-engine/internal/llm/openai"
	"github.com/atheory-ai/context-engine/internal/orggraph"
	"github.com/atheory-ai/context-engine/internal/plugins"
	pluginruntime "github.com/atheory-ai/context-engine/internal/plugins/runtime"
	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

// Engine is the assembled, ready-to-use context engine.
// The zero value is invalid — construct with New().
type Engine struct {
	cfg        *config.Config
	channels   *core.AppChannels
	dbRegistry *db.Registry
	buffer     writebuffer.Buffer
	substrate  *substrate.ReadWriter
	plugins    *plugins.Registry
	llmRouter  *llm.Router
	orgGraph   *orggraph.OrgGraph

	pluginCandidates []pluginCandidate
	pluginRoot       string

	iirOnce      sync.Once
	iirExtractor iir.Extractor

	// A project graph becomes authoritative only at the end of Index. Keep one
	// local run at a time and reject readers while replacement is in progress.
	indexMu sync.Mutex
}

// New constructs a fully wired Engine from config.
// Creates the data directory if needed, opens all databases, runs migrations,
// starts the write buffer goroutine, and loads plugins. Call Close() when done.
func New(ctx context.Context, cfg *config.Config) (*Engine, error) {
	ch := core.NewAppChannels()
	e := &Engine{
		cfg:      cfg,
		channels: &ch,
	}

	// ── Ensure data directories exist ───────────────────────────────────────
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "graphs"), 0755); err != nil {
		return nil, fmt.Errorf("create graphs dir: %w", err)
	}

	// ── Open databases and run migrations ────────────────────────────────────
	e.dbRegistry = db.NewRegistry()

	metaPath := filepath.Join(cfg.DataDir, "meta.db")
	if err := e.dbRegistry.OpenMeta(metaPath); err != nil {
		return nil, fmt.Errorf("open meta.db: %w", err)
	}
	if err := migrations.RunMeta(e.dbRegistry.Meta()); err != nil {
		return nil, fmt.Errorf("migrate meta.db: %w", err)
	}

	auditPath := filepath.Join(cfg.DataDir, "audit.db")
	if err := e.dbRegistry.OpenAudit(auditPath); err != nil {
		return nil, fmt.Errorf("open audit.db: %w", err)
	}
	if err := migrations.RunAudit(e.dbRegistry.Audit()); err != nil {
		return nil, fmt.Errorf("migrate audit.db: %w", err)
	}

	if cfg.Tracing.Enabled {
		execPath := filepath.Join(cfg.DataDir, "execution.db")
		if err := e.dbRegistry.OpenExecution(execPath); err != nil {
			return nil, fmt.Errorf("open execution.db: %w", err)
		}
		if err := migrations.RunExecution(e.dbRegistry.Exec()); err != nil {
			return nil, fmt.Errorf("migrate execution.db: %w", err)
		}
	}

	orgPath := filepath.Join(cfg.DataDir, "graphs", "org.db")
	if err := e.dbRegistry.OpenOrgGraph(orgPath); err != nil {
		return nil, fmt.Errorf("open org graph: %w", err)
	}
	orgDB, err := e.dbRegistry.GraphDB("org")
	if err != nil {
		return nil, fmt.Errorf("get org graph db: %w", err)
	}
	if err := migrations.RunGraph(orgDB); err != nil {
		return nil, fmt.Errorf("migrate org.db: %w", err)
	}
	if err := migrations.RunOrg(orgDB); err != nil {
		return nil, fmt.Errorf("migrate org.db (org): %w", err)
	}
	e.orgGraph = orggraph.OpenFromDB(orgDB)

	// An index run mounts the local graph only for its own process. Every later
	// CLI, MCP, or API runner must remount that persisted graph before serving
	// deterministic context tools.
	if err := e.mountPersistedLocalGraph(); err != nil {
		return nil, err
	}

	// ── Start write buffer goroutine ─────────────────────────────────────────
	// The buffer is engine infrastructure, not work owned by the caller's
	// request context. In particular, an interrupted `ce index` must retain a
	// live writer long enough for the indexer to abort/flush and mark its run
	// failed. Engine.Close owns this lifecycle.
	e.buffer = writebuffer.New(context.Background(), e.dbRegistry,
		writebuffer.DefaultBufferSize,
		writebuffer.DefaultFlushInterval,
	)

	// ── Build substrate read/write layer ─────────────────────────────────────
	e.substrate = substrate.NewReadWriter(e.dbRegistry, e.buffer)

	// ── Extract embedded default plugins ─────────────────────────────────────
	if err := indexer.ExtractDefaults(cfg.DataDir); err != nil {
		return nil, fmt.Errorf("extract default plugins: %w", err)
	}

	// ── Load plugins ─────────────────────────────────────────────────────────
	e.plugins = plugins.NewRegistry()
	if err := e.plugins.Initialize(cfg.DataDir, e.channels); err != nil {
		return nil, fmt.Errorf("initialize plugin runtime: %w", err)
	}
	e.plugins.SetAllowDevStreamPlugins(cfg.Features.AllowDevStreamPlugins)
	e.plugins.SetIndexPoolSize(cfg.Indexer.ExtractWorkers)

	// Catalog defaults and configured plugins without starting their WASM
	// runtimes. Index activation selects the language/dependency closure needed
	// by the project; installed plugins remain available for future activation.
	defaultsDir := filepath.Join(cfg.DataDir, "plugins", "defaults")
	e.pluginCandidates, err = catalogPluginCandidates(defaultsDir, cfg.Plugins)
	if err != nil {
		return nil, fmt.Errorf("catalog plugins: %w", err)
	}

	// ── Build LLM router ─────────────────────────────────────────────────────
	e.llmRouter = buildLLMRouter(cfg)

	return e, nil
}

// mountPersistedLocalGraph makes an existing local substrate available to a
// newly constructed engine. A missing graph is valid before the first index;
// callers will receive the existing "run ce index first" error when they try
// to use graph-backed tools.
func (e *Engine) mountPersistedLocalGraph() error {
	const projectID = "local"
	path := filepath.Join(e.cfg.DataDir, "graphs", projectID+".db")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat persisted project graph: %w", err)
	}
	if err := e.dbRegistry.Mount(projectID, path); err != nil {
		return fmt.Errorf("mount persisted project graph: %w", err)
	}
	graphDB, err := e.dbRegistry.GraphDB(projectID)
	if err != nil {
		return fmt.Errorf("get persisted project graph: %w", err)
	}
	if err := migrations.RunGraph(graphDB); err != nil {
		return fmt.Errorf("migrate persisted project graph: %w", err)
	}
	return nil
}

// NewLLMProvider builds a standalone model provider from config, without opening
// databases, starting the write buffer, or loading plugins. It is for model use
// outside the engine (e.g. IIR shaping, which needs no substrate). Returns nil
// if no router could be built, so callers' nil checks are not defeated by a
// typed-nil interface.
func NewLLMProvider(cfg *config.Config) core.LLMProvider {
	r := buildLLMRouter(cfg)
	if r == nil {
		return nil
	}
	return r
}

// buildLLMRouter constructs the LLM router from config.
// Checks CE_LLM_API_KEY and ANTHROPIC_API_KEY env vars as fallbacks.
func buildLLMRouter(cfg *config.Config) *llm.Router {
	apiKey := cfg.LLM.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("CE_LLM_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	anthropicProv := anthropic.New(anthropic.Config{
		APIKey:  apiKey,
		BaseURL: cfg.LLM.BaseURL,
	})

	// OpenAI falls back to OPENAI_API_KEY when the shared key is unset.
	openaiKey := apiKey
	if openaiKey == "" {
		openaiKey = os.Getenv("OPENAI_API_KEY")
	}
	openaiProv := openai.New(openai.Config{
		APIKey:     openaiKey,
		BaseURL:    cfg.LLM.BaseURL,
		MaxRetries: cfg.LLM.MaxRetries,
	})

	// Local Ollama uses BaseURL as its server endpoint (default localhost:11434).
	localProv := local.New(local.Config{
		BaseURL: cfg.LLM.BaseURL,
		Timeout: time.Duration(cfg.LLM.TimeoutSeconds) * time.Second,
	})

	llmCfg := llm.Config{
		DefaultProvider: cfg.LLM.Provider,
		TierModels:      cfg.LLM.Models,
		Anthropic: llm.AnthropicConfig{
			APIKey:  apiKey,
			BaseURL: cfg.LLM.BaseURL,
		},
	}

	return llm.NewRouter(llmCfg,
		llm.ProviderEntry{Name: "anthropic", Provider: anthropicProv},
		llm.ProviderEntry{Name: "openai", Provider: openaiProv},
		llm.ProviderEntry{Name: "local", Provider: localProv},
	)
}

// QueryOptions carries the query and optional overrides for a query run.
type QueryOptions struct {
	Query    string
	MaxLoops int // 0 = use project/config default
}

// QueryResult is the outcome of a synchronous query run.
type QueryResult struct {
	RunID      string
	Answer     string
	TokensIn   int
	TokensOut  int
	CostUSD    float64
	LoopsUsed  int
	DurationMS int64
	Partial    bool
}

// Query executes the cognitive loop for a user query.
// Emits to channels as work progresses.
// Blocks until the answer is synthesized or an error occurs.
func (e *Engine) Query(ctx context.Context, query string) error {
	if err := e.requireReadyCorpus(ctx); err != nil {
		return err
	}
	dag := e.buildDAG()
	return dag.Run(ctx, query)
}

// NewChannels creates a fresh AppChannels set for a dedicated query run.
// Used by WebSocket handler and QuerySync so each connection gets its own channels.
func (e *Engine) NewChannels() *core.AppChannels {
	ch := core.NewAppChannels()
	return &ch
}

// CloseChannels is a no-op for now — channels are garbage collected.
// Provided for symmetry with NewChannels and future cleanup hooks.
func (e *Engine) CloseChannels(_ *core.AppChannels) {}

// QueryWithChannels runs a query emitting to caller-supplied channels.
// The WebSocket handler calls this so it can stream to its own client.
func (e *Engine) QueryWithChannels(
	ctx context.Context,
	query string,
	ch *core.AppChannels,
	opts QueryOptions,
) error {
	if err := e.requireReadyCorpus(ctx); err != nil {
		return err
	}
	dag := e.buildDAG()
	_ = opts // MaxLoops applied by resolveMaxLoops via IR; future: inject into runContext
	return dag.RunWithChannels(ctx, query, ch)
}

// QuerySync runs a query synchronously and returns the complete result.
// Used by MCP tools and REST API POST /api/v1/query.
func (e *Engine) QuerySync(ctx context.Context, opts QueryOptions) (*QueryResult, error) {
	ch := e.NewChannels()
	defer e.CloseChannels(ch)

	start := time.Now()

	// Accumulate answer text from the message channel in a goroutine.
	var (
		answer string
		mu     sync.Mutex
		wg     sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case msg, ok := <-ch.Message:
				if !ok {
					return
				}
				mu.Lock()
				answer = msg.Content // last message wins (synthesizer emits once)
				mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	err := e.QueryWithChannels(ctx, opts.Query, ch, opts)

	// Signal the drainer to stop, then wait.
	close(ch.Message)
	wg.Wait()

	if err != nil {
		return nil, err
	}

	mu.Lock()
	ans := answer
	mu.Unlock()

	return &QueryResult{
		Answer:     ans,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

// ProjectStatusResult holds a summary of the active project's index state.
type ProjectStatusResult struct {
	GitURL       string
	IndexState   string
	NodeCount    int
	EdgeCount    int
	FilesIndexed int
	LastIndexed  time.Time
}

// ProjectStatus returns the index status of the active project.
func (e *Engine) ProjectStatus(ctx context.Context) (*ProjectStatusResult, error) {
	project, err := queries.GetProject(ctx, e.dbRegistry.Meta(), "local")
	if err != nil {
		return nil, fmt.Errorf("project status: %w", err)
	}
	if project == nil {
		return nil, fmt.Errorf("no active project — run 'ce project init' first")
	}

	result := &ProjectStatusResult{
		GitURL:     project.GitURL,
		IndexState: project.Status,
	}

	// Best-effort: count nodes and edges from the project graph.
	if graphDB, err := e.dbRegistry.GraphDB("local"); err == nil {
		var n int
		_ = graphDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE project_id = 'local'`).Scan(&n) //nolint:errcheck // best-effort stats; n=0 on failure is fine
		result.NodeCount = n
		var eg int
		_ = graphDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges WHERE project_id = 'local'`).Scan(&eg) //nolint:errcheck // see comment above
		result.EdgeCount = eg
	}

	if project.LastIndexedAt.Valid {
		result.LastIndexed = time.UnixMilli(project.LastIndexedAt.Int64)
	}

	return result, nil
}

// SearchOptions carries parameters for a lightweight substrate search.
type SearchOptions struct {
	Query string
	Type  string // optional node type filter
	Limit int
}

// SearchNode is a minimal node result for the search response.
type SearchNode struct {
	ID          string
	Type        string
	Label       string
	CanonicalID string
	SourceClass string
	FilePath    string
	LineStart   int
	LineEnd     int
	Score       int
	MatchReason string
}

// SearchSubstrate performs a lightweight node search without running the cognitive loop.
// It tokenizes multi-term queries and ranks label, canonical ID, file path, and
// source range matches so agents can chain directly into source inspection.
func (e *Engine) SearchSubstrate(ctx context.Context, opts SearchOptions) ([]SearchNode, error) {
	if err := e.requireReadyCorpus(ctx); err != nil {
		return nil, err
	}
	graphDB, err := e.dbRegistry.GraphDB("local")
	if err != nil {
		return nil, fmt.Errorf("search substrate: graph not available — run 'ce index' first")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	var args []any
	var conds []string

	conds = append(conds, "project_id = 'local'")
	tokens := searchTokens(opts.Query)
	if len(tokens) == 0 {
		return nil, nil
	}
	var tokenConds []string
	for _, token := range tokens {
		term := "%" + token + "%"
		tokenConds = append(tokenConds, "(canonical_id LIKE ? OR label LIKE ? OR json_extract(properties, '$.file_path') LIKE ?)")
		args = append(args, term, term, term)
	}
	conds = append(conds, "("+strings.Join(tokenConds, " OR ")+")")

	if opts.Type != "" {
		conds = append(conds, "type = ?")
		args = append(args, opts.Type)
	}
	candidateLimit := limit * 12
	if candidateLimit < 100 {
		candidateLimit = 100
	}
	args = append(args, candidateLimit)

	// #nosec G202 -- conds is built from a fixed set of programmer-defined
	// SQL fragments with `?` placeholders; values are bound separately via args.
	q := `SELECT id, type, label, canonical_id, source_class, properties FROM nodes WHERE ` +
		strings.Join(conds, " AND ") + ` ORDER BY length(canonical_id) LIMIT ?`

	rows, err := graphDB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search substrate: %w", err)
	}
	defer rows.Close()

	var nodes []SearchNode
	for rows.Next() {
		var n SearchNode
		var propertiesJSON string
		if err := rows.Scan(&n.ID, &n.Type, &n.Label, &n.CanonicalID, &n.SourceClass, &propertiesJSON); err != nil {
			return nil, fmt.Errorf("search substrate scan: %w", err)
		}
		var props map[string]any
		if err := json.Unmarshal([]byte(propertiesJSON), &props); err != nil {
			props = map[string]any{}
		}
		n.FilePath = stringProperty(props, "file_path")
		n.LineStart, n.LineEnd = nodeLineRange(props)
		n.Score, n.MatchReason = scoreSearchNode(n, tokens)
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Score == nodes[j].Score {
			return len(nodes[i].CanonicalID) < len(nodes[j].CanonicalID)
		}
		return nodes[i].Score > nodes[j].Score
	})
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return nodes, nil
}

// Index walks rootDir, extracts nodes and edges via language plugins,
// and writes them to the project's substrate graph.
// full=true makes the complete plugin-owned graph authoritative. full=false
// replaces output only for changed/deleted files. Both modes retain a prior
// corpus only until the replacement transaction succeeds.
func (e *Engine) Index(ctx context.Context, rootDir string, full bool) (indexer.Stats, error) {
	return e.index(ctx, rootDir, full, nil)
}

// IndexPaths incrementally replaces the derived contribution for only the
// supplied paths. Paths omitted from the request remain authoritative; missing
// requested paths remove their old contribution. This is used by --watch so a
// single save does not walk the complete corpus.
func (e *Engine) IndexPaths(ctx context.Context, rootDir string, paths []string) (indexer.Stats, error) {
	return e.index(ctx, rootDir, false, paths)
}

func (e *Engine) index(ctx context.Context, rootDir string, full bool, paths []string) (indexer.Stats, error) {
	const projectID = core.ProjectID("local")
	if !e.indexMu.TryLock() {
		return indexer.Stats{}, fmt.Errorf("index already in progress")
	}
	defer e.indexMu.Unlock()
	if err := e.activateIndexPlugins(ctx, rootDir); err != nil {
		return indexer.Stats{}, err
	}
	published := false
	defer func() {
		if !published {
			if err := queries.UpdateProjectStatus(context.Background(), e.dbRegistry.Meta(), string(projectID), "stale"); err != nil {
				e.channels.Emit(core.Emission{Source: "runner", Channel: core.ChanWarning, Content: fmt.Sprintf("mark project stale: %v", err)})
			}
		}
	}()

	now := time.Now().UnixMilli()
	if err := queries.UpsertProject(ctx, e.dbRegistry.Meta(), queries.Project{ID: string(projectID), GitURL: e.cfg.Project.GitURL, Name: filepath.Base(rootDir), Status: "indexing", CreatedAt: now, LastSeenAt: now, Properties: "{}"}); err != nil {
		return indexer.Stats{}, fmt.Errorf("mark project indexing: %w", err)
	}
	if err := queries.UpsertProjectPath(ctx, e.dbRegistry.Meta(), string(projectID), rootDir, now); err != nil {
		return indexer.Stats{}, fmt.Errorf("record project path: %w", err)
	}

	// Ensure the project graph DB is open and migrated.
	localDBPath := filepath.Join(e.cfg.DataDir, "graphs", "local.db")
	if err := e.dbRegistry.Mount(string(projectID), localDBPath); err != nil {
		return indexer.Stats{}, fmt.Errorf("mount project graph: %w", err)
	}
	localDB, err := e.dbRegistry.GraphDB(string(projectID))
	if err != nil {
		return indexer.Stats{}, fmt.Errorf("get project graph: %w", err)
	}
	if err := migrations.RunGraph(localDB); err != nil {
		return indexer.Stats{}, fmt.Errorf("migrate project graph: %w", err)
	}

	// Create IndexQueries for file hash tracking (incremental reindex).
	iqr := queries.NewIndexQueries(localDB)

	// Build and run the indexer.
	idx := indexer.New(e.cfg, e.plugins, e.substrate, iqr, e.channels)
	var stats indexer.Stats
	var runErr error
	if paths != nil {
		stats, runErr = idx.RunPaths(ctx, rootDir, projectID, paths)
	} else {
		stats, runErr = idx.Run(ctx, rootDir, projectID, full)
	}

	// Flush the write buffer (indexer.Run also flushes, but be safe).
	if flushErr := e.buffer.Flush(ctx); flushErr != nil && runErr == nil {
		return stats, fmt.Errorf("flush write buffer: %w", flushErr)
	}
	if runErr == nil {
		// Bulk indexing may have expanded every active plugin pool. The run is
		// complete, so retain one warm instance per plugin for watch latency and
		// release the surplus high-water linear memories.
		e.plugins.TrimIndexPools()
	}

	// Publish readiness only after graph reconciliation has succeeded.
	if runErr == nil {
		now := time.Now().UnixMilli()
		_ = queries.UpsertProject(ctx, e.dbRegistry.Meta(), queries.Project{ //nolint:errcheck // graph is already authoritative; metadata recovery is safe
			ID:         string(projectID),
			GitURL:     e.cfg.Project.GitURL,
			Name:       filepath.Base(rootDir),
			Status:     "indexed",
			CreatedAt:  now,
			LastSeenAt: now,
			Properties: "{}",
		})
		_ = queries.UpsertProjectPath(ctx, e.dbRegistry.Meta(), string(projectID), rootDir, now) //nolint:errcheck // see comment above
		_ = queries.UpdateLastIndexedAt(ctx, e.dbRegistry.Meta(), string(projectID), now)        //nolint:errcheck // see comment above
		published = true

		// Lift indexed nodes/edges into the org graph, then detect cross-project edges.
		if liftErr := e.orgGraph.Lift(ctx, projectID, localDB); liftErr != nil {
			e.channels.Emit(core.Emission{
				Source:  "runner",
				Channel: core.ChanWarning,
				Content: fmt.Sprintf("org lift: %v", liftErr),
			})
		} else {
			if crossErr := e.orgGraph.DetectCrossProjectEdges(ctx, projectID); crossErr != nil {
				e.channels.Emit(core.Emission{
					Source:  "runner",
					Channel: core.ChanWarning,
					Content: fmt.Sprintf("cross-project detection: %v", crossErr),
				})
			}
		}
	}

	return stats, runErr
}

func (e *Engine) requireReadyCorpus(ctx context.Context) error {
	// Small package-level consumers construct a read-only Engine around a graph
	// registry without the runner's metadata database. They have no indexing
	// lifecycle to guard; a fully constructed Engine always has meta.db.
	if e.dbRegistry == nil || e.dbRegistry.Meta() == nil {
		return nil
	}
	if !e.indexMu.TryLock() {
		return fmt.Errorf("project index is in progress; retry when it completes")
	}
	e.indexMu.Unlock()
	project, err := queries.GetProject(ctx, e.dbRegistry.Meta(), "local")
	if err != nil {
		return fmt.Errorf("check project index status: %w", err)
	}
	if project == nil || project.Status != "indexed" {
		return fmt.Errorf("project corpus is %s; run a successful 'ce index' before querying", projectStatus(project))
	}
	return nil
}

func projectStatus(project *queries.Project) string {
	if project == nil {
		return "unindexed"
	}
	return project.Status
}

// Channels returns the AppChannels for this engine.
// The caller reads from these to render output.
func (e *Engine) Channels() *core.AppChannels {
	return e.channels
}

// IIRRulePack returns the effective IIR rule pack: the built-in defaults with
// any plugin-contributed rule packs merged over them (loaded plugins declare
// their rule "flavours" in their manifest). A plugin pack that fails to load is
// skipped with a warning. Consumed by the verify surfaces (MCP/API).
func (e *Engine) IIRRulePack() iir.RulePack {
	pack, errs := iir.EffectiveRulePack(e.plugins.IIRRulePackJSONs())
	for _, err := range errs {
		e.channels.Emit(core.Emission{
			Source:  "iir",
			Channel: core.ChanWarning,
			Content: fmt.Sprintf("plugin IIR rule pack: %v", err),
		})
	}
	return pack
}

// defaultPluginNames are the built-in plugin wasm files available under
// <dataDir>/plugins/defaults. Their metadata is cataloged without loading
// WASM; configured user plugins retain higher registration priority.
var defaultPluginNames = []string{"go-language.wasm", "typescript.wasm", "python.wasm"}

type pluginCandidate struct {
	path     string
	config   map[string]any
	manifest pluginruntime.PluginManifest
}

func catalogPluginCandidates(defaultsDir string, entries []config.PluginEntry) ([]pluginCandidate, error) {
	candidates := make([]pluginCandidate, 0, len(defaultPluginNames)+len(entries))
	for _, builtin := range builtinPluginCatalog(defaultsDir) {
		if _, err := os.Stat(builtin.path); err == nil {
			manifest, err := pluginruntime.ReadCatalogManifest(builtin.path)
			if err != nil {
				return nil, fmt.Errorf("default %s: %w", builtin.path, err)
			}
			builtin.manifest = manifest.PluginManifest
			candidates = append(candidates, builtin)
		}
	}
	for _, entry := range entries {
		manifest, err := pluginruntime.ReadCatalogManifest(entry.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Path, err)
		}
		candidate := pluginCandidate{path: entry.Path, config: entry.Config, manifest: manifest.PluginManifest}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func builtinPluginCatalog(dir string) []pluginCandidate {
	return []pluginCandidate{
		{path: filepath.Join(dir, "go-language.wasm"), manifest: pluginruntime.PluginManifest{ID: "com.atheory-ai.go-language", Capabilities: pluginruntime.PluginCapabilities{Language: true}, Language: &pluginruntime.PluginLanguageInfo{Name: "go", Extensions: []string{".go"}}, Provides: []string{"language:go"}}},
		{path: filepath.Join(dir, "typescript.wasm"), manifest: pluginruntime.PluginManifest{ID: "com.atheory-ai.typescript", Capabilities: pluginruntime.PluginCapabilities{Language: true}, Language: &pluginruntime.PluginLanguageInfo{Name: "typescript", Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs"}}, Provides: []string{"language:typescript"}}},
		{path: filepath.Join(dir, "python.wasm"), manifest: pluginruntime.PluginManifest{ID: "com.atheory-ai.python", Capabilities: pluginruntime.PluginCapabilities{Language: true}, Language: &pluginruntime.PluginLanguageInfo{Name: "python", Extensions: []string{".py", ".pyi"}}, Provides: []string{"language:python"}}},
	}
}

// activateIndexPlugins instantiates only candidates selected by explicit
// project configuration or by the extensions actually present in rootDir. A
func (e *Engine) activateIndexPlugins(ctx context.Context, rootDir string) error {
	if e.pluginRoot == rootDir {
		return nil
	}
	if e.pluginRoot != "" {
		e.plugins.UnloadAll()
		if err := e.plugins.Initialize(e.cfg.DataDir, e.channels); err != nil {
			return fmt.Errorf("reinitialize plugin runtime: %w", err)
		}
		e.plugins.SetAllowDevStreamPlugins(e.cfg.Features.AllowDevStreamPlugins)
		e.plugins.SetIndexPoolSize(e.cfg.Indexer.ExtractWorkers)
	}

	extensions := projectExtensions(rootDir)
	wanted := make(map[string]struct{}, len(e.cfg.PluginActivation.Enabled))
	for _, id := range e.cfg.PluginActivation.Enabled {
		wanted[id] = struct{}{}
	}
	explicitSelection := len(wanted) > 0
	wordpressProfile := e.cfg.PluginActivation.Profile == "wordpress"
	selected := make([]bool, len(e.pluginCandidates))
	for i, candidate := range e.pluginCandidates {
		if _, ok := wanted[candidate.manifest.ID]; ok {
			selected[i] = true
			continue
		}
		if explicitSelection {
			continue
		}
		if wordpressProfile {
			selected[i] = candidateMatchesExtensions(candidate.manifest, map[string]struct{}{".php": {}})
			continue
		}
		selected[i] = candidateMatchesExtensions(candidate.manifest, extensions)
	}
	// Complete manifest dependencies so selecting an enricher also activates
	// its language/CST provider even when the provider's extension differs.
	for changed := true; changed; {
		changed = false
		for i, candidate := range e.pluginCandidates {
			if !selected[i] {
				continue
			}
			for _, required := range append(append([]string{}, candidate.manifest.Requires...), languageRequirements(candidate.manifest.Enriches)...) {
				for j, provider := range e.pluginCandidates {
					if selected[j] || !provides(provider.manifest, required) {
						continue
					}
					selected[j], changed = true, true
				}
			}
		}
	}
	for i, candidate := range e.pluginCandidates {
		if !selected[i] {
			continue
		}
		if err := e.plugins.Load(ctx, candidate.path, candidate.config); err != nil {
			return fmt.Errorf("activate plugin %s: %w", candidate.path, err)
		}
	}
	e.pluginRoot = rootDir
	return nil
}

func candidateMatchesExtensions(manifest pluginruntime.PluginManifest, extensions map[string]struct{}) bool {
	if manifest.Language == nil {
		return false
	}
	for _, extension := range manifest.Language.Extensions {
		if _, ok := extensions[strings.ToLower(extension)]; ok {
			return true
		}
	}
	return false
}

func projectExtensions(root string) map[string]struct{} {
	extensions := map[string]struct{}{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			if err == nil && entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			if extension := strings.ToLower(filepath.Ext(path)); extension != "" {
				extensions[extension] = struct{}{}
			}
		}
		return nil
	})
	return extensions
}

func languageRequirements(languages []string) []string {
	required := make([]string, len(languages))
	for i, language := range languages {
		required[i] = "language:" + language
	}
	return required
}

func provides(manifest pluginruntime.PluginManifest, capability string) bool {
	for _, provided := range manifest.Provides {
		if provided == capability {
			return true
		}
	}
	return false
}

// PluginRulePacks loads the configured plugins and returns their contributed IIR
// rule-pack JSON, plus a cleanup func to unload them. It is best-effort and
// standalone: it does not open the substrate or DBs, so lightweight surfaces
// (the CLI verify commands) can pick up plugin "flavours" without a full engine.
// If the plugin runtime can't be initialized (e.g. no data dir), it returns nil
// packs and a no-op cleanup so callers fall back to the built-in defaults.
func PluginRulePacks(ctx context.Context, cfg *config.Config, ch *core.AppChannels) (packs [][]byte, cleanup func()) {
	noop := func() {}
	reg := plugins.NewRegistry()
	if err := reg.Initialize(cfg.DataDir, ch); err != nil {
		return nil, noop
	}
	reg.SetAllowDevStreamPlugins(cfg.Features.AllowDevStreamPlugins)

	defaultsDir := filepath.Join(cfg.DataDir, "plugins", "defaults")
	for _, name := range defaultPluginNames {
		path := filepath.Join(defaultsDir, name)
		if _, err := os.Stat(path); err != nil {
			continue // not built/installed — skip
		}
		if err := reg.Load(ctx, path, nil); err != nil {
			ch.Emit(core.Emission{Source: "iir", Channel: core.ChanWarning,
				Content: fmt.Sprintf("default plugin %s: %v", name, err)})
		}
	}
	for _, entry := range cfg.Plugins {
		if err := reg.Load(ctx, entry.Path, entry.Config); err != nil {
			ch.Emit(core.Emission{Source: "iir", Channel: core.ChanWarning,
				Content: fmt.Sprintf("plugin %s: %v", entry.Path, err)})
		}
	}
	return reg.IIRRulePackJSONs(), reg.UnloadAll
}

// ActiveProjectPath returns the file system path of the active project.
// Returns empty string if no project path is recorded in meta.db.
func (e *Engine) ActiveProjectPath() string {
	paths, err := queries.ListProjectPaths(context.Background(), e.dbRegistry.Meta(), "local")
	if err != nil || len(paths) == 0 {
		return ""
	}
	return paths[0].Path
}

// InsertToken creates a new API token in meta.db.
func (e *Engine) InsertToken(ctx context.Context, t queries.Token) error {
	return queries.InsertToken(ctx, e.dbRegistry.Meta(), t)
}

// ListTokens returns all API tokens from meta.db.
func (e *Engine) ListTokens(ctx context.Context) ([]queries.Token, error) {
	return queries.ListTokens(ctx, e.dbRegistry.Meta())
}

// RevokeToken marks a token as revoked in meta.db.
func (e *Engine) RevokeToken(ctx context.Context, id string) error {
	return queries.RevokeToken(ctx, e.dbRegistry.Meta(), id, time.Now().UnixMilli())
}

// Close flushes the write buffer, closes all databases, unloads plugins.
func (e *Engine) Close(ctx context.Context) error {
	if err := e.buffer.Close(ctx); err != nil {
		return fmt.Errorf("close write buffer: %w", err)
	}
	e.plugins.UnloadAll()
	// orgGraph is OpenFromDB — Close() is a no-op (dbRegistry owns the connection).
	// Called here for explicitness and to allow future owned-DB mode.
	_ = e.orgGraph.Close()
	return e.dbRegistry.CloseAll()
}
