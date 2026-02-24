# Context Engine — Spec 1: Data Layer
## Implementation Spec — Runnable DDL + Storage Architecture
### Version 1.0 | February 2026

---

> This is an implementation spec, not a PRD section. Every table here gets built.
> Hand this document to Claude Code as the working context for the storage package.
> Companion documents: Context Engine PRD v0.5 (Sections 15, 16.9, 16.11, 16.12),
> Decisions Log v1.0 (Section 1).

---

## 1. File Layout

All databases live under `~/.ce/`. The directory is created on first run if absent.

```
~/.ce/
  meta.db                  — token store + project registry (always open)
  audit.db                 — audit log, turn tracking (always open)
  execution.db             — verbatim LLM call log (always open, write conditional)
  graphs/
    org.db                 — org-level substrate graph (always open)
    <git-url-hash>.db      — per-project substrate graph (mounted on demand)
```

### Why these files are separate

`meta.db` and `audit.db` are always open, always small, low-write-frequency. They
never grow unbounded and are safe to back up independently.

`execution.db` can grow large in development mode. Separate file means it can be
cleared or rotated without touching any other database.

`graphs/` are the performance-critical files. Per-project files mount and unmount
independently. The org graph is always mounted. A project graph is opened when
that project is registered as active for a session and closed when the session ends.

---

## 2. Global Conventions

### WAL Mode
Every database opens with WAL mode enabled. This is non-negotiable — concurrent
readers must not block the write buffer goroutine.

```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;    -- safe with WAL, faster than FULL
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;     -- 5s timeout before SQLITE_BUSY error
```

These PRAGMAs are applied on every connection open, before any queries.

### Node ID Generation — Deterministic Hash

Node IDs are `sha256(project_id + ":" + node_type + ":" + canonical_identifier)`
truncated to 16 bytes, hex-encoded (32 character string).

```go
func NodeID(projectID, nodeType, canonicalID string) string {
    h := sha256.Sum256([]byte(projectID + ":" + nodeType + ":" + canonicalID))
    return hex.EncodeToString(h[:16])
}
```

`canonicalID` is the fully-qualified identifier within the project:
- For symbols: `file/path/from/root:FunctionName`
- For namespaces: `full/package/path`
- For concepts: `lowercase-term`
- For files: `file/path/from/root`

Same logical entity always produces the same ID. Indexer can upsert safely.
Write buffer deduplication is trivially correct.

Edge IDs follow the same pattern:
```go
func EdgeID(sourceID, edgeType, targetID string) string {
    h := sha256.Sum256([]byte(sourceID + ":" + edgeType + ":" + targetID))
    return hex.EncodeToString(h[:16])
}
```

### Timestamps

All timestamps are Unix epoch milliseconds stored as INTEGER. No TEXT dates.
Go: `time.Now().UnixMilli()`

### Properties Column

All `properties` columns are `TEXT` containing valid JSON. Empty properties
are stored as `'{}'` not NULL. SQLite JSON functions work on TEXT columns:

```sql
SELECT * FROM nodes WHERE json_extract(properties, '$.file_path') = 'src/billing.go'
```

---

## 3. meta.db — Token Store + Project Registry

```sql
-- ============================================================
-- meta.db
-- Token store and project registry.
-- Always open. Small. Rarely written.
-- ============================================================

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL,
    applied_at  INTEGER NOT NULL
);

INSERT INTO schema_version (version, applied_at) VALUES (1, unixepoch() * 1000);

-- ============================================================
-- Project Registry
-- Primary key: git remote URL (normalized).
-- Filesystem path is not the identity.
-- ============================================================

CREATE TABLE IF NOT EXISTS projects (
    id              TEXT PRIMARY KEY,        -- UUID v4
    git_url         TEXT NOT NULL UNIQUE,    -- normalized git remote URL
    name            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'unindexed',
                                             -- unindexed | indexed | stale
    graph_path      TEXT,                   -- absolute path to <hash>.db
    config_hash     TEXT,                   -- hash of ce.yaml at last index
    base_prompt     TEXT,                   -- high-level project context
    arch_prompt     TEXT,                   -- architectural detail prompt
    created_at      INTEGER NOT NULL,
    last_seen_at    INTEGER NOT NULL,
    last_indexed_at INTEGER,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_projects_git_url ON projects(git_url);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);

-- Known local filesystem paths for this project.
-- A project may exist at multiple paths (worktrees, multiple machines via sync).
CREATE TABLE IF NOT EXISTS project_paths (
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    last_seen   INTEGER NOT NULL,
    PRIMARY KEY (project_id, path)
);

CREATE INDEX IF NOT EXISTS idx_project_paths_path ON project_paths(path);

-- ============================================================
-- Token Store
-- Local token management. No external auth service.
-- ============================================================

CREATE TABLE IF NOT EXISTS tokens (
    id          TEXT PRIMARY KEY,           -- UUID v4, the token value
    name        TEXT NOT NULL,              -- human-readable label
    scope       TEXT NOT NULL,              -- read | read-write | admin
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER,                    -- NULL = no expiry
    last_used   INTEGER,
    revoked     INTEGER NOT NULL DEFAULT 0, -- 0 = active, 1 = revoked
    revoked_at  INTEGER,
    properties  TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_tokens_scope ON tokens(scope);
CREATE INDEX IF NOT EXISTS idx_tokens_revoked ON tokens(revoked);
```

---

## 4. audit.db — Audit Log + Turn Tracking

```sql
-- ============================================================
-- audit.db
-- Authoritative record of who did what when.
-- Append-only. Never update or delete rows.
-- Separate from the cognitive trace archive.
-- ============================================================

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL,
    applied_at  INTEGER NOT NULL
);

INSERT INTO schema_version (version, applied_at) VALUES (1, unixepoch() * 1000);

-- ============================================================
-- Sessions
-- Every interaction belongs to a session.
-- ============================================================

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,       -- UUID v4
    actor_id        TEXT NOT NULL,          -- identity (local user or token name)
    token_id        TEXT,                   -- NULL for local sessions
    surface         TEXT NOT NULL,          -- cli | tui | mcp | api | ws
    started_at      INTEGER NOT NULL,
    last_active_at  INTEGER NOT NULL,
    ended_at        INTEGER,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_sessions_actor ON sessions(actor_id);
CREATE INDEX IF NOT EXISTS idx_sessions_surface ON sessions(surface);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at);

-- ============================================================
-- Turns
-- One complete user interaction: input → final answer.
-- All engine actions within a turn share a turn_id.
-- Enables full reconstruction of any interaction.
-- ============================================================

CREATE TABLE IF NOT EXISTS turns (
    id              TEXT PRIMARY KEY,       -- UUID v4
    session_id      TEXT NOT NULL REFERENCES sessions(id),
    query           TEXT,                   -- user's input text
    started_at      INTEGER NOT NULL,
    ended_at        INTEGER,
    loop_count      INTEGER,
    status          TEXT NOT NULL DEFAULT 'active',
                                            -- active | complete | cancelled | error
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id);
CREATE INDEX IF NOT EXISTS idx_turns_started ON turns(started_at);
CREATE INDEX IF NOT EXISTS idx_turns_status ON turns(status);

-- ============================================================
-- Audit Entries
-- Every engine action recorded. Append-only.
-- ============================================================

CREATE TABLE IF NOT EXISTS audit_entries (
    id              TEXT PRIMARY KEY,       -- UUID v4
    session_id      TEXT NOT NULL,
    turn_id         TEXT,                   -- NULL for session-level actions
    actor_id        TEXT NOT NULL,
    token_id        TEXT,
    on_behalf_of    TEXT,                   -- MCP proxy: human user identity
    surface         TEXT NOT NULL,
    action          TEXT NOT NULL,          -- query | index | token_create | etc.
    project_ids     TEXT,                   -- JSON array of project IDs
    scope           TEXT,                   -- token scope at time of action
    status          TEXT NOT NULL DEFAULT 'ok', -- ok | error | cancelled
    error_message   TEXT,
    timestamp       INTEGER NOT NULL,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_audit_session ON audit_entries(session_id);
CREATE INDEX IF NOT EXISTS idx_audit_turn ON audit_entries(turn_id);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_entries(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_entries(action);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_entries(timestamp);
```

---

## 5. execution.db — Verbatim LLM Call Log

```sql
-- ============================================================
-- execution.db
-- Verbatim record of every LLM call.
-- Written in development mode always.
-- Written in production only when --trace flag is active.
-- NEVER written for read-scoped token sessions.
-- Primary consumer: CE Studio, plugin sandbox CLI.
-- Schema version is a BREAKING CHANGE contract.
-- ============================================================

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL,
    applied_at  INTEGER NOT NULL
);

-- Version 1 — initial schema. Bump on any column change.
INSERT INTO schema_version (version, applied_at) VALUES (1, unixepoch() * 1000);

CREATE TABLE IF NOT EXISTS execution_log (
    id                  TEXT PRIMARY KEY,   -- UUID v4
    run_id              TEXT NOT NULL,      -- groups all entries for one query
    turn_id             TEXT NOT NULL,
    session_id          TEXT NOT NULL,
    trace_id            TEXT,               -- links to cognitive trace archive

    -- Cognitive position
    node_type           TEXT NOT NULL,
    -- strategizer | reviewer | synthesizer | preflight | tool:{name} | router
    loop_index          INTEGER NOT NULL DEFAULT 0,
    is_replay           INTEGER NOT NULL DEFAULT 0,  -- 1 if Studio replay

    -- LLM call (verbatim — never truncated)
    model               TEXT NOT NULL,
    tier                TEXT NOT NULL,      -- fast | standard | thinking
    prompt_messages     TEXT NOT NULL,      -- JSON array of {role, content}
    response            TEXT NOT NULL,      -- full response text
    thinking_trace      TEXT,               -- extended reasoning, NULL if absent
    ir_emitted          TEXT,               -- JSON IR struct if Strategizer entry

    -- Metrics
    tokens_in           INTEGER NOT NULL DEFAULT 0,
    tokens_out          INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd  REAL NOT NULL DEFAULT 0,
    latency_ms          INTEGER NOT NULL DEFAULT 0,
    context_used_pct    REAL NOT NULL DEFAULT 0,

    -- Substrate impact
    nodes_created       TEXT NOT NULL DEFAULT '[]',  -- JSON array of node IDs
    edges_updated       TEXT NOT NULL DEFAULT '[]',  -- JSON array of edge IDs
    source_transitions  TEXT NOT NULL DEFAULT '[]',  -- JSON array

    timestamp           INTEGER NOT NULL,
    properties          TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_exec_run_id ON execution_log(run_id);
CREATE INDEX IF NOT EXISTS idx_exec_turn_id ON execution_log(turn_id);
CREATE INDEX IF NOT EXISTS idx_exec_session ON execution_log(session_id);
CREATE INDEX IF NOT EXISTS idx_exec_node_type ON execution_log(node_type);
CREATE INDEX IF NOT EXISTS idx_exec_timestamp ON execution_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_exec_model ON execution_log(model);
```

---

## 6. Substrate Graph — Per-Project Schema

This schema is used for both `org.db` and each `<git-url-hash>.db`.
The org graph uses `project_id = 'org'` by convention.

```sql
-- ============================================================
-- <project-hash>.db  (and org.db)
-- Property graph substrate.
-- Palantir object graph model: relationships (edges) are
-- first-class. Nodes represent anything a plugin defines.
-- ============================================================

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL,
    applied_at  INTEGER NOT NULL
);

INSERT INTO schema_version (version, applied_at) VALUES (1, unixepoch() * 1000);

-- ============================================================
-- Nodes
-- Immutable after indexing (except activation via separate table).
-- Node ID: sha256(project_id:type:canonical_id)[:16] hex-encoded.
-- ============================================================

CREATE TABLE IF NOT EXISTS nodes (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    type            TEXT NOT NULL,
    -- Core built-in types: symbol | namespace | concept | file | directory
    -- Plugin-defined types: anything (stored as-is, no enum constraint)
    label           TEXT NOT NULL,          -- human-readable display name
    canonical_id    TEXT NOT NULL,          -- fully-qualified identifier
    source_class    TEXT NOT NULL DEFAULT 'structural',
    -- structural | associative | speculative | derived
    plugin_id       TEXT,                   -- which plugin created this node
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    properties      TEXT NOT NULL DEFAULT '{}'
    -- Properties vary by node type. Common fields:
    -- symbol:    { file_path, line_start, line_end, signature, language }
    -- namespace: { path, language, file_count }
    -- concept:   { definition, related_terms: [], synonym_for }
    -- file:      { path, language, size_bytes, last_modified }
);

CREATE INDEX IF NOT EXISTS idx_nodes_project ON nodes(project_id);
CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);
CREATE INDEX IF NOT EXISTS idx_nodes_canonical ON nodes(project_id, canonical_id);
CREATE INDEX IF NOT EXISTS idx_nodes_label ON nodes(label);
CREATE INDEX IF NOT EXISTS idx_nodes_source_class ON nodes(source_class);
CREATE INDEX IF NOT EXISTS idx_nodes_plugin ON nodes(plugin_id);

-- ============================================================
-- Node Activation
-- Separated from node rows — write buffer primary target.
-- High-frequency writes during cognitive loop.
-- Node rows are stable; activation is volatile.
-- ============================================================

CREATE TABLE IF NOT EXISTS node_activation (
    node_id         TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    activation      REAL NOT NULL DEFAULT 0.0,
    peak_activation REAL NOT NULL DEFAULT 0.0,  -- highest seen this session
    updated_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_node_activation_level
    ON node_activation(activation DESC);

-- ============================================================
-- Edges
-- Immutable after indexing (except weight via separate table).
-- Edge ID: sha256(source_id:type:target_id)[:16] hex-encoded.
-- Edges are first-class. Relationships are the product.
-- ============================================================

CREATE TABLE IF NOT EXISTS edges (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    source_id       TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id       TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    type            TEXT NOT NULL,
    -- Core built-in types:
    --   calls | imports | implements | extends | contains |
    --   references | defines | belongs_to | synonym_of |
    --   co_activates | annotates
    -- Plugin-defined types: anything
    source_class    TEXT NOT NULL DEFAULT 'structural',
    -- structural | associative | speculative | derived
    plugin_id       TEXT,
    created_at      INTEGER NOT NULL,
    properties      TEXT NOT NULL DEFAULT '{}'
    -- Properties vary by edge type. Common fields:
    -- calls:     { call_site_line, call_site_file, is_async }
    -- imports:   { alias, is_star_import }
    -- implements:{ interface_name }
);

CREATE INDEX IF NOT EXISTS idx_edges_project ON edges(project_id);
CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type);
CREATE INDEX IF NOT EXISTS idx_edges_source_class ON edges(source_class);
-- Composite index for the most common query pattern:
-- "give me all edges of type X from source Y"
CREATE INDEX IF NOT EXISTS idx_edges_source_type
    ON edges(source_id, type);
CREATE INDEX IF NOT EXISTS idx_edges_target_type
    ON edges(target_id, type);

-- ============================================================
-- Edge Weight
-- Separated from edge rows — write buffer primary target.
-- Updated by Hebbian learning during cognitive loops.
-- source_class here can drift from edge.source_class as
-- learning promotes speculative edges to associative.
-- ============================================================

CREATE TABLE IF NOT EXISTS edge_weight (
    edge_id             TEXT PRIMARY KEY REFERENCES edges(id) ON DELETE CASCADE,
    weight              REAL NOT NULL DEFAULT 1.0,
    source_class        TEXT NOT NULL DEFAULT 'structural',
    co_activation_count INTEGER NOT NULL DEFAULT 0,
    last_co_activation  INTEGER,
    updated_at          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_edge_weight_weight
    ON edge_weight(weight DESC);
CREATE INDEX IF NOT EXISTS idx_edge_weight_source_class
    ON edge_weight(source_class);

-- ============================================================
-- Concept Seeds
-- Org-level and project-level ontology entries.
-- Pre-loaded vocabulary that improves pre-flight recognition.
-- ============================================================

CREATE TABLE IF NOT EXISTS concept_seeds (
    id          TEXT PRIMARY KEY,
    term        TEXT NOT NULL,
    scope       TEXT NOT NULL DEFAULT 'project', -- project | org
    definition  TEXT,
    related     TEXT NOT NULL DEFAULT '[]',  -- JSON array of related terms
    synonyms    TEXT NOT NULL DEFAULT '[]',  -- JSON array of synonyms
    source      TEXT NOT NULL DEFAULT 'manual',
    -- manual | plugin | derived | llm-enriched
    plugin_id   TEXT,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_concept_seeds_term
    ON concept_seeds(scope, term);
CREATE INDEX IF NOT EXISTS idx_concept_seeds_source
    ON concept_seeds(source);

-- ============================================================
-- Index Runs
-- Record of each indexing pass for a project.
-- Enables stale detection and incremental reindex.
-- ============================================================

CREATE TABLE IF NOT EXISTS index_runs (
    id              TEXT PRIMARY KEY,       -- UUID v4
    project_id      TEXT NOT NULL,
    plugin_ids      TEXT NOT NULL DEFAULT '[]', -- JSON array
    started_at      INTEGER NOT NULL,
    completed_at    INTEGER,
    status          TEXT NOT NULL DEFAULT 'running',
    -- running | complete | failed | cancelled
    nodes_created   INTEGER NOT NULL DEFAULT 0,
    nodes_updated   INTEGER NOT NULL DEFAULT 0,
    edges_created   INTEGER NOT NULL DEFAULT 0,
    files_processed INTEGER NOT NULL DEFAULT 0,
    error_message   TEXT,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_index_runs_project ON index_runs(project_id);
CREATE INDEX IF NOT EXISTS idx_index_runs_status ON index_runs(status);
CREATE INDEX IF NOT EXISTS idx_index_runs_started ON index_runs(started_at);

-- ============================================================
-- Enrichments
-- Substrate changes made by the Reviewer during cognitive loops.
-- Provenance record: why a node or edge was created/updated.
-- ============================================================

CREATE TABLE IF NOT EXISTS enrichments (
    id              TEXT PRIMARY KEY,       -- UUID v4
    run_id          TEXT NOT NULL,
    turn_id         TEXT NOT NULL,
    loop_index      INTEGER NOT NULL,
    entity_type     TEXT NOT NULL,          -- node | edge | concept_seed
    entity_id       TEXT NOT NULL,
    action          TEXT NOT NULL,          -- created | updated | promoted
    -- promoted = source_class upgraded (speculative → associative)
    before_state    TEXT,                   -- JSON snapshot before change
    after_state     TEXT NOT NULL,          -- JSON snapshot after change
    rationale       TEXT,                   -- Reviewer's stated reason
    created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_enrichments_run ON enrichments(run_id);
CREATE INDEX IF NOT EXISTS idx_enrichments_entity ON enrichments(entity_id);
CREATE INDEX IF NOT EXISTS idx_enrichments_turn ON enrichments(turn_id);
```

---

## 7. Write Buffer — Go Interface + Implementation Sketch

The write buffer is a single-writer goroutine that owns all writes to substrate
graph databases. Callers fire-and-forget — they never block waiting for write
confirmation. The buffer flushes on two triggers: buffer full OR time elapsed.

### The Write Operation Types

```go
// storage/writebuffer/types.go

package writebuffer

type OpType string

const (
    OpUpsertNode       OpType = "upsert_node"
    OpUpsertEdge       OpType = "upsert_edge"
    OpUpdateActivation OpType = "update_activation"
    OpUpdateWeight     OpType = "update_weight"
    OpUpsertConcept    OpType = "upsert_concept"
    OpRecordEnrichment OpType = "record_enrichment"
)

// WriteOp is the unit of work the buffer accepts.
type WriteOp struct {
    Type      OpType
    ProjectID string  // determines which graph DB to write to
    Payload   any     // typed per OpType (see below)
}

// Activation updates are the most frequent write.
// The buffer deduplicates: if node X has 5 pending activation
// updates, only the final value is written.
type ActivationUpdate struct {
    NodeID     string
    Activation float64
    UpdatedAt  int64
}

// Edge weight updates from Hebbian learning.
type WeightUpdate struct {
    EdgeID           string
    Weight           float64
    SourceClass      string
    CoActivationDelta int // added to existing count, not replaced
    UpdatedAt        int64
}

type NodeUpsert struct {
    ID          string
    ProjectID   string
    Type        string
    Label       string
    CanonicalID string
    SourceClass string
    PluginID    string
    Properties  string // JSON
    CreatedAt   int64
    UpdatedAt   int64
}

type EdgeUpsert struct {
    ID          string
    ProjectID   string
    SourceID    string
    TargetID    string
    Type        string
    SourceClass string
    PluginID    string
    Properties  string // JSON
    CreatedAt   int64
}
```

### The Buffer Interface

```go
// storage/writebuffer/buffer.go

package writebuffer

import (
    "context"
    "time"
)

const (
    DefaultBufferSize    = 1024        // ops before forced flush
    DefaultFlushInterval = 50 * time.Millisecond
)

// Buffer is the single-writer goroutine interface.
// Callers obtain a Buffer from New() and call Send().
// Send never blocks (buffered channel). If the channel
// is full, Send returns ErrBufferFull — callers should
// log this and continue, not retry in a tight loop.
type Buffer interface {
    // Send enqueues a write operation. Non-blocking.
    Send(op WriteOp) error

    // Flush forces an immediate flush. Blocks until complete.
    // Used at the end of a query turn to ensure all writes land.
    Flush(ctx context.Context) error

    // Close flushes remaining ops and shuts down the goroutine.
    Close(ctx context.Context) error
}

// New creates a Buffer and starts the writer goroutine.
// dbProvider resolves a project ID to the appropriate *sql.DB.
// The goroutine runs until Close is called.
func New(
    ctx context.Context,
    dbProvider DBProvider,
    bufSize int,
    flushInterval time.Duration,
) Buffer

// DBProvider resolves a project ID to its graph database.
// "org" returns the org graph. Any registered project ID
// returns that project's graph.
type DBProvider interface {
    GraphDB(projectID string) (*sql.DB, error)
}
```

### Deduplication Logic

The buffer maintains a pending map keyed by `(opType, projectID, entityID)`.
When a new op arrives for the same key:

- **ActivationUpdate**: replace with the new value (last write wins)
- **WeightUpdate**: accumulate `CoActivationDelta`, replace weight with latest
- **NodeUpsert / EdgeUpsert**: replace entirely (idempotent by design — same ID)
- **RecordEnrichment**: always append (no deduplication — each enrichment is distinct)

On flush, the pending map is drained, ops are batched into a single SQLite
transaction per database, and the map is cleared.

```go
// Pseudocode for the writer goroutine

func (b *buffer) run(ctx context.Context) {
    ticker := time.NewTicker(b.flushInterval)
    defer ticker.Stop()

    for {
        select {
        case op := <-b.ch:
            b.pending.merge(op)  // deduplicate
            if len(b.pending) >= b.bufSize {
                b.flush(ctx)
            }

        case <-ticker.C:
            if len(b.pending) > 0 {
                b.flush(ctx)
            }

        case <-b.flushReq:
            b.flush(ctx)
            b.flushDone <- struct{}{}

        case <-ctx.Done():
            b.flush(ctx)  // drain before exit
            return
        }
    }
}
```

---

## 8. Connection Management

### Opening a Database

```go
// storage/db/open.go

package db

import (
    "database/sql"
    "fmt"
    _ "github.com/mattn/go-sqlite3"
)

// Open opens a SQLite database with the standard CE pragmas applied.
// path should be the absolute path to the .db file.
// For in-memory databases (tests), use ":memory:".
func Open(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON&_busy_timeout=5000")
    if err != nil {
        return nil, fmt.Errorf("open %s: %w", path, err)
    }
    // SQLite performs best with a single writer.
    // Multiple readers are fine with WAL mode.
    db.SetMaxOpenConns(1)
    db.SetMaxIdleConns(1)
    return db, nil
}

// OpenReadOnly opens a database for read-only access.
// Multiple read-only connections are safe with WAL mode.
func OpenReadOnly(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite3", path+"?mode=ro&_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000")
    if err != nil {
        return nil, fmt.Errorf("open readonly %s: %w", path, err)
    }
    db.SetMaxOpenConns(10)  // readers can be concurrent
    db.SetMaxIdleConns(5)
    return db, nil
}
```

### The Graph Database Registry

```go
// storage/db/registry.go

// Registry manages all open database connections.
// It is the DBProvider that the write buffer uses.
type Registry struct {
    mu       sync.RWMutex
    orgDB    *sql.DB
    projects map[string]*sql.DB  // projectID → *sql.DB
    metaDB   *sql.DB
    auditDB  *sql.DB
    execDB   *sql.DB
}

// Mount opens a project graph database and registers it.
// Safe to call multiple times for the same project (idempotent).
func (r *Registry) Mount(projectID, dbPath string) error

// Unmount closes and deregisters a project graph database.
func (r *Registry) Unmount(projectID string) error

// GraphDB implements DBProvider. Returns org DB for "org".
func (r *Registry) GraphDB(projectID string) (*sql.DB, error)
```

---

## 9. Migrations — golang-migrate

Migration files are embedded in the Go binary using `go:embed`. They run
automatically at startup if the schema version is behind.

### Directory Structure

```
storage/
  migrations/
    meta/
      000001_initial.up.sql
      000001_initial.down.sql
    audit/
      000001_initial.up.sql
      000001_initial.down.sql
    execution/
      000001_initial.up.sql
      000001_initial.down.sql
    graph/
      000001_initial.up.sql
      000001_initial.down.sql
```

### Embedding and Running

```go
// storage/migrations/migrate.go

package migrations

import (
    "embed"
    "fmt"

    "github.com/golang-migrate/migrate/v4"
    "github.com/golang-migrate/migrate/v4/database/sqlite3"
    "github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed meta/* audit/* execution/* graph/*
var migrationFiles embed.FS

// RunMeta applies pending migrations to meta.db.
func RunMeta(db *sql.DB) error {
    return runMigrations(db, "meta")
}

func RunAudit(db *sql.DB) error {
    return runMigrations(db, "audit")
}

func RunExecution(db *sql.DB) error {
    return runMigrations(db, "execution")
}

func RunGraph(db *sql.DB) error {
    return runMigrations(db, "graph")
}

func runMigrations(db *sql.DB, name string) error {
    src, err := iofs.New(migrationFiles, name)
    if err != nil {
        return fmt.Errorf("migration source %s: %w", name, err)
    }
    driver, err := sqlite3.WithInstance(db, &sqlite3.Config{})
    if err != nil {
        return fmt.Errorf("migration driver %s: %w", name, err)
    }
    m, err := migrate.NewWithInstance("iofs", src, name, driver)
    if err != nil {
        return fmt.Errorf("migrate %s: %w", name, err)
    }
    if err := m.Up(); err != nil && err != migrate.ErrNoChange {
        return fmt.Errorf("migrate up %s: %w", name, err)
    }
    return nil
}
```

---

## 10. Test Strategy

### In-Memory Databases

All storage tests use in-memory SQLite. No files created, no cleanup needed.

```go
func TestMain(m *testing.M) {
    // In-memory db with same pragmas as production
    db, _ := db.Open(":memory:")
    migrations.RunGraph(db)
    // ... tests run
}
```

### Write Buffer Tests

Test the deduplication logic directly by sending multiple ops for the same
entity and asserting only the final state is written:

```go
func TestActivationDeduplication(t *testing.T) {
    buf := writebuffer.New(ctx, testProvider, 1024, 10*time.Millisecond)

    // Send 10 activation updates for the same node
    for i := 0; i < 10; i++ {
        buf.Send(WriteOp{
            Type:    OpUpdateActivation,
            Payload: ActivationUpdate{NodeID: "node1", Activation: float64(i)},
        })
    }

    buf.Flush(ctx)

    // Assert only one row exists with activation = 9.0 (last value)
    var activation float64
    db.QueryRow("SELECT activation FROM node_activation WHERE node_id = ?", "node1").
        Scan(&activation)
    assert.Equal(t, 9.0, activation)
}
```

### Property Graph Queries

Include tests for the most common activation queries to validate index usage:

```go
// Top-K activated nodes across a project
SELECT n.id, n.type, n.label, na.activation
FROM nodes n
JOIN node_activation na ON na.node_id = n.id
WHERE n.project_id = ?
ORDER BY na.activation DESC
LIMIT ?

// All edges from an activated node above threshold
SELECT e.*, ew.weight, ew.source_class
FROM edges e
JOIN edge_weight ew ON ew.edge_id = e.id
JOIN node_activation na ON na.node_id = e.source_id
WHERE e.project_id = ?
  AND na.activation > ?
ORDER BY ew.weight DESC
```

These two query patterns are the hot path during activation propagation.
They must use indexes, not full table scans. Verify with `EXPLAIN QUERY PLAN`.

---

## 11. Storage Package Layout

```
storage/
  db/
    open.go          — Open, OpenReadOnly
    registry.go      — Registry (connection manager, DBProvider)
  writebuffer/
    buffer.go        — Buffer interface + New constructor
    types.go         — WriteOp, ActivationUpdate, WeightUpdate, etc.
    pending.go       — pending map deduplication logic
    buffer_test.go
  migrations/
    migrate.go       — RunMeta, RunAudit, RunExecution, RunGraph
    meta/            — meta.db migration SQL files
    audit/           — audit.db migration SQL files
    execution/       — execution.db migration SQL files
    graph/           — graph DB migration SQL files
  queries/
    nodes.go         — typed query functions for node operations
    edges.go         — typed query functions for edge operations
    activation.go    — activation read/write (via write buffer)
    enrichments.go   — enrichment record queries
    projects.go      — project registry queries
    tokens.go        — token store queries
    sessions.go      — session + turn queries
    audit.go         — audit entry queries
    execution.go     — execution log queries
```

All `queries/` functions take a `*sql.DB` or `*sqlx.DB` and return typed structs.
They never open connections themselves. Connection management belongs to `db/`.

---

## 12. Key Decisions Captured in This Spec

| Decision | Value | Rationale |
|----------|-------|-----------|
| Graph model | Property graph | Flexible, plugin-extensible, uniform activation queries |
| Node ID | Deterministic hash | Idempotent upserts, free deduplication |
| Properties | TEXT/JSON | SQLite json_extract, no binary overhead |
| Activation storage | Separate table | Isolates high-frequency writes to write buffer domain |
| Edge weight storage | Separate table | Same — Hebbian updates isolated from stable edge rows |
| ORM | None — raw sql + sqlx | No hidden queries, write buffer owns all writes |
| Migrations | golang-migrate embedded | Standard pattern, embedded in binary, auto-runs on startup |
| WAL mode | All databases | Concurrent readers don't block writer goroutine |
| Connection limit | 1 writer, N readers | SQLite best practice with WAL |

---

*Spec 1: Data Layer — v1.0 — February 2026*
*Next: Spec 2 — Go Package Structure*
*Companion: Context Engine PRD v0.5 Section 15, Decisions Log v1.0 Section 1*
