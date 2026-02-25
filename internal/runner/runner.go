// Package runner is the cognitive loop engine.
// It wires the static DAG, executes the loop, manages concurrency,
// tracks the token budget, and handles all exit conditions.
//
// The entry point is New() → Engine. Callers call Query() to run a query.
package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/graph/substrate"
	"github.com/atheory/context-engine/internal/indexer"
	"github.com/atheory/context-engine/internal/llm"
	"github.com/atheory/context-engine/internal/llm/anthropic"
	"github.com/atheory/context-engine/internal/plugins"
	"github.com/atheory/context-engine/internal/storage/db"
	"github.com/atheory/context-engine/internal/storage/migrations"
	"github.com/atheory/context-engine/internal/storage/queries"
	"github.com/atheory/context-engine/internal/storage/writebuffer"
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

	// ── Start write buffer goroutine ─────────────────────────────────────────
	e.buffer = writebuffer.New(ctx, e.dbRegistry,
		writebuffer.DefaultBufferSize,
		writebuffer.DefaultFlushInterval,
	)

	// ── Build substrate read/write layer ─────────────────────────────────────
	e.substrate = substrate.NewReadWriter(e.dbRegistry, e.buffer)

	// ── Load plugins ─────────────────────────────────────────────────────────
	e.plugins = plugins.NewRegistry()
	if err := e.plugins.Initialize(cfg.DataDir, e.channels); err != nil {
		return nil, fmt.Errorf("initialize plugin runtime: %w", err)
	}
	if err := e.plugins.LoadAll(ctx, toPluginEntries(cfg.Plugins)); err != nil {
		return nil, fmt.Errorf("load plugins: %w", err)
	}

	// ── Build LLM router ─────────────────────────────────────────────────────
	e.llmRouter = buildLLMRouter(cfg)

	return e, nil
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

	llmCfg := llm.LLMConfig{
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

// toPluginEntries converts config plugin entries to plugins.PluginEntry.
func toPluginEntries(cfgEntries []config.PluginEntry) []plugins.PluginEntry {
	entries := make([]plugins.PluginEntry, len(cfgEntries))
	for i, e := range cfgEntries {
		entries[i] = plugins.PluginEntry{Path: e.Path, Config: e.Config}
	}
	return entries
}

// Query executes the cognitive loop for a user query.
// Emits to channels as work progresses.
// Blocks until the answer is synthesized or an error occurs.
func (e *Engine) Query(ctx context.Context, query string) error {
	dag := e.buildDAG()
	return dag.Run(ctx, query)
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

	// Merge CLI-level include/exclude overrides with the config defaults.
	idxCfg := e.cfg.Indexer

	// Build and run the indexer.
	idx := indexer.New(
		indexer.Config{
			RootDir:    rootDir,
			ProjectID:  projectID,
			IndexerCfg: idxCfg,
			Full:       full,
		},
		e.plugins.Loaded(),
		e.substrate,
		e.channels,
	)

	stats, runErr := idx.Run(ctx)

	// Flush the write buffer so all writes are committed before we return.
	if flushErr := e.buffer.Flush(ctx); flushErr != nil {
		return stats, fmt.Errorf("flush write buffer: %w", flushErr)
	}

	// Update last_indexed_at in meta.db (best-effort — don't mask index errors).
	if runErr == nil {
		_ = queries.UpdateLastIndexedAt(ctx, e.dbRegistry.Meta(), string(projectID), time.Now().UnixMilli())
	}

	return stats, runErr
}

// Channels returns the AppChannels for this engine.
// The caller reads from these to render output.
func (e *Engine) Channels() *core.AppChannels {
	return e.channels
}

// Close flushes the write buffer, closes all databases, unloads plugins.
func (e *Engine) Close(ctx context.Context) error {
	if err := e.buffer.Close(ctx); err != nil {
		return fmt.Errorf("close write buffer: %w", err)
	}
	e.plugins.UnloadAll()
	return e.dbRegistry.CloseAll()
}
