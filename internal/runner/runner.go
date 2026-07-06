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
	"github.com/atheory-ai/context-engine/internal/orggraph"
	"github.com/atheory-ai/context-engine/internal/plugins"
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

	// ── Start write buffer goroutine ─────────────────────────────────────────
	e.buffer = writebuffer.New(ctx, e.dbRegistry,
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

	// Load default plugins first (lowest priority — user plugins can override).
	defaultsDir := filepath.Join(cfg.DataDir, "plugins", "defaults")
	for _, name := range []string{"go-language.wasm", "typescript.wasm", "python.wasm", "php.wasm", "wordpress-conventions.wasm", "woocommerce-conventions.wasm"} {
		path := filepath.Join(defaultsDir, name)
		if _, err := os.Stat(path); err != nil {
			continue // not yet built (development) — skip
		}
		if err := e.plugins.Load(ctx, path, nil); err != nil {
			e.channels.Emit(core.Emission{
				Source:  "runner",
				Channel: core.ChanWarning,
				Content: fmt.Sprintf("default plugin %s: %v", name, err),
			})
		}
	}

	// Load user-installed plugins (higher priority — override defaults).
	for _, entry := range cfg.Plugins {
		if err := e.plugins.Load(ctx, entry.Path, entry.Config); err != nil {
			return nil, fmt.Errorf("load plugin %s: %w", entry.Path, err)
		}
	}

	// ── Build LLM router ─────────────────────────────────────────────────────
	e.llmRouter = buildLLMRouter(cfg)

	return e, nil
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

	llmCfg := llm.Config{
		DefaultProvider: cfg.LLM.Provider,
		TierModels:      cfg.LLM.Models,
		Anthropic: llm.AnthropicConfig{
			APIKey:  apiKey,
			BaseURL: cfg.LLM.BaseURL,
		},
	}

	return llm.NewRouter(llmCfg, llm.ProviderEntry{
		Name:     "anthropic",
		Provider: anthropicProv,
	})
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
// full=true forces a complete reindex regardless of previous state.
// Phase 1: incremental indexing is not yet implemented; full is always performed.
func (e *Engine) Index(ctx context.Context, rootDir string, full bool) (indexer.Stats, error) {
	const projectID = core.ProjectID("local")

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
	stats, runErr := idx.Run(ctx, rootDir, projectID, full)

	// Flush the write buffer (indexer.Run also flushes, but be safe).
	if flushErr := e.buffer.Flush(ctx); flushErr != nil && runErr == nil {
		return stats, fmt.Errorf("flush write buffer: %w", flushErr)
	}

	// Update project record in meta.db (best-effort — don't mask index errors).
	if runErr == nil {
		now := time.Now().UnixMilli()
		_ = queries.UpsertProject(ctx, e.dbRegistry.Meta(), queries.Project{ //nolint:errcheck // metadata best-effort; index already succeeded
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
