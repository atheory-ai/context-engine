# Context Engine — Spec 17: Org Graph
## Implementation Spec — Cross-Project Substrate, Org-Level Indexing, Multi-Project Intelligence
### Version 1.0 | February 2026

---

> This spec covers the org graph — the substrate layer above individual projects.
> Hand to Claude Code alongside spec-1-data-layer.md, spec-9-indexer.md,
> and spec-10-activation.md.
> Companion: Context Engine PRD v0.5 Section 12. Decisions Log v1.0 Section 13.

---

## 1. Overview

Every project has its own substrate graph — nodes and edges extracted from
that project's codebase. The org graph is the layer above: a single graph
that spans all projects, populated by lifting nodes and edges upward from
project graphs.

The org graph enables:

- **Cross-project queries** — "what other services call this API"
- **Organization-wide concepts** — concept seeds that apply to the whole org
  rather than a single project
- **Dependency mapping** — which projects depend on which shared libraries
- **Pattern recognition** — the same architectural pattern appearing in
  multiple projects surfaces in the org graph as a strong concept

The crossproject tool (Spec 11) queries the org graph. Without this spec,
it always returns empty results.

---

## 2. Architecture

```
~/.ce/
  meta.db          ← project registry (already exists, Spec 1)
  org.db           ← org graph (new — this spec)
  projects/
    <project-id>/
      graph.db     ← per-project substrate
```

The org graph database (`org.db`) has the same schema as a project `graph.db`
but spans all projects. `project_id` in every row identifies which project
a node or edge came from.

---

## 3. Org Graph Schema

```sql
-- migrations/org/000001_initial.up.sql
-- Org graph uses the same schema as project graph.db,
-- but project_id is populated with the source project ID.

-- These tables are identical to graph.db schema from Spec 1.
-- Reproduced here for clarity — org.db is a separate file.

CREATE TABLE IF NOT EXISTS nodes (
    id           TEXT NOT NULL,
    project_id   TEXT NOT NULL,   -- source project ID
    type         TEXT NOT NULL,
    label        TEXT NOT NULL,
    canonical_id TEXT NOT NULL,
    source_class TEXT NOT NULL DEFAULT 'structural',
    plugin_id    TEXT,
    properties   TEXT NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS edges (
    id           TEXT NOT NULL,
    project_id   TEXT NOT NULL,
    source_id    TEXT NOT NULL,
    target_id    TEXT NOT NULL,
    type         TEXT NOT NULL,
    source_class TEXT NOT NULL DEFAULT 'structural',
    plugin_id    TEXT,
    properties   TEXT NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS edge_weight (
    edge_id              TEXT NOT NULL,
    weight               REAL NOT NULL DEFAULT 0.5,
    source_class         TEXT NOT NULL DEFAULT 'structural',
    co_activation_count  INTEGER NOT NULL DEFAULT 0,
    updated_at           INTEGER NOT NULL,
    PRIMARY KEY (edge_id)
);

CREATE TABLE IF NOT EXISTS node_activation (
    node_id         TEXT NOT NULL,
    activation      REAL NOT NULL DEFAULT 0.0,
    peak_activation REAL NOT NULL DEFAULT 0.0,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (node_id)
);

-- Org-specific: concept seeds that apply across all projects
CREATE TABLE IF NOT EXISTS org_concept_seeds (
    term        TEXT NOT NULL PRIMARY KEY,
    definition  TEXT,
    synonyms    TEXT NOT NULL DEFAULT '[]',  -- JSON array
    related     TEXT NOT NULL DEFAULT '[]',  -- JSON array
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- Cross-project edges connecting nodes from different projects
CREATE TABLE IF NOT EXISTS cross_project_edges (
    id              TEXT NOT NULL PRIMARY KEY,
    source_node_id  TEXT NOT NULL,   -- node ID in source project
    source_project  TEXT NOT NULL,
    target_node_id  TEXT NOT NULL,   -- node ID in target project
    target_project  TEXT NOT NULL,
    type            TEXT NOT NULL,   -- "shares_interface" | "depends_on" | "mirrors"
    source_class    TEXT NOT NULL DEFAULT 'speculative',
    weight          REAL NOT NULL DEFAULT 0.5,
    properties      TEXT NOT NULL DEFAULT '{}',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_org_nodes_canonical
    ON nodes(canonical_id);
CREATE INDEX IF NOT EXISTS idx_org_nodes_type
    ON nodes(type, project_id);
CREATE INDEX IF NOT EXISTS idx_org_nodes_label
    ON nodes(label);
CREATE INDEX IF NOT EXISTS idx_org_edges_source
    ON edges(source_id, project_id);
CREATE INDEX IF NOT EXISTS idx_org_edges_target
    ON edges(target_id, project_id);
CREATE INDEX IF NOT EXISTS idx_cross_edges_source
    ON cross_project_edges(source_node_id, source_project);
CREATE INDEX IF NOT EXISTS idx_cross_edges_target
    ON cross_project_edges(target_node_id, target_project);
```

---

## 4. What Gets Lifted to the Org Graph

Not everything from a project graph belongs in the org graph. Lifting
everything would make the org graph a noisy duplicate of all projects.

**Lifted (exported to org graph):**

| Node/Edge type | Why |
|----------------|-----|
| Namespace nodes | Package/module structure is org-relevant |
| Exported symbol nodes | Public API surfaces |
| Interface nodes | Contracts that other projects might implement |
| Concept nodes | Domain vocabulary |
| imports edges | Dependency graph between packages |
| implements edges | Interface implementations |
| extends edges | Class hierarchies |
| concept edges | Concept relationships |

**Not lifted:**

| Node/Edge type | Why |
|----------------|-----|
| File nodes | Internal file structure, not org-relevant |
| Internal (unexported) symbols | Private implementation details |
| method_of edges | Redundant given exported methods are lifted |
| defined_in edges | File paths aren't meaningful across projects |
| calls edges | Too noisy, project-specific execution paths |

---

## 5. Package Structure

```
internal/orggraph/
  orggraph.go       — OrgGraph struct, Lift(), FindSimilar()
  schema.go         — org.db migration management
  lift.go           — project graph → org graph lifting logic
  crossproject.go   — cross-project edge detection
  queries/
    nodes.go        — node queries against org.db
    edges.go        — edge queries against org.db
    concepts.go     — org concept seed management
```

---

## 6. OrgGraph

```go
// internal/orggraph/orggraph.go

package orggraph

// OrgGraph manages the org-level substrate.
type OrgGraph struct {
    db      *sql.DB
    queries *queries.OrgQueries
}

func Open(dataDir string) (*OrgGraph, error) {
    dbPath := filepath.Join(dataDir, "org.db")
    db, err := sql.Open("sqlite3", dbPath+"?"+sqliteParams)
    if err != nil {
        return nil, fmt.Errorf("open org.db: %w", err)
    }

    if err := runMigrations(db); err != nil {
        return nil, fmt.Errorf("org.db migrations: %w", err)
    }

    return &OrgGraph{
        db:      db,
        queries: queries.New(db),
    }, nil
}

// Lift copies eligible nodes and edges from a project graph into the org graph.
// Called after every successful full or incremental index.
// Idempotent — safe to call multiple times.
func (g *OrgGraph) Lift(ctx context.Context, projectID core.ProjectID, projectDB *sql.DB) error {
    lifter := &Lifter{
        projectID: projectID,
        src:       projectDB,
        dst:       g.db,
    }
    return lifter.Run(ctx)
}

// FindSimilar finds nodes in the org graph similar to the given canonical ID.
// Used by the crossproject tool (Spec 11).
func (g *OrgGraph) FindSimilar(
    ctx         context.Context,
    canonicalID string,
    nodeType    string,
    limit       int,
) ([]OrgMatch, error) {
    return g.queries.FindSimilar(ctx, canonicalID, nodeType, limit)
}

// GetOrgConceptSeeds returns concept seeds defined at the org level.
// These supplement per-project concept seeds.
func (g *OrgGraph) GetOrgConceptSeeds(ctx context.Context) ([]core.ConceptSeed, error) {
    return g.queries.GetConceptSeeds(ctx)
}

// AddOrgConceptSeed adds or updates an org-level concept seed.
func (g *OrgGraph) AddOrgConceptSeed(ctx context.Context, seed core.ConceptSeed) error {
    return g.queries.UpsertConceptSeed(ctx, seed)
}
```

---

## 7. Lifting Logic

```go
// internal/orggraph/lift.go

// Lifter copies eligible nodes and edges from project graph to org graph.
type Lifter struct {
    projectID core.ProjectID
    src       *sql.DB  // project graph.db
    dst       *sql.DB  // org.db
}

func (l *Lifter) Run(ctx context.Context) error {
    // Run in a transaction for atomicity
    tx, err := l.dst.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Lift nodes
    if err := l.liftNodes(ctx, tx); err != nil {
        return fmt.Errorf("lift nodes: %w", err)
    }

    // Lift edges (only between lifted nodes)
    if err := l.liftEdges(ctx, tx); err != nil {
        return fmt.Errorf("lift edges: %w", err)
    }

    return tx.Commit()
}

func (l *Lifter) liftNodes(ctx context.Context, tx *sql.Tx) error {
    // Fetch eligible nodes from project graph
    rows, err := l.src.QueryContext(ctx, `
        SELECT id, type, label, canonical_id, source_class,
               plugin_id, properties, created_at, updated_at
        FROM nodes
        WHERE project_id = ?
          AND (
            -- Namespaces
            type = 'namespace'
            -- Exported symbols
            OR (type = 'symbol' AND json_extract(properties, '$.exported') = true)
            -- Interfaces
            OR (type = 'symbol' AND json_extract(properties, '$.kind') = 'interface')
            -- Concepts
            OR type = 'concept'
          )
    `, string(l.projectID))
    if err != nil {
        return err
    }
    defer rows.Close()

    stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO nodes (id, project_id, type, label, canonical_id,
                          source_class, plugin_id, properties, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            label        = excluded.label,
            source_class = excluded.source_class,
            properties   = excluded.properties,
            updated_at   = excluded.updated_at
    `)
    if err != nil {
        return err
    }
    defer stmt.Close()

    for rows.Next() {
        var n dbNode
        if err := rows.Scan(&n.id, &n.nodeType, &n.label, &n.canonicalID,
            &n.sourceClass, &n.pluginID, &n.properties,
            &n.createdAt, &n.updatedAt); err != nil {
            return err
        }

        _, err := stmt.ExecContext(ctx,
            n.id, string(l.projectID), n.nodeType, n.label, n.canonicalID,
            n.sourceClass, n.pluginID, n.properties,
            n.createdAt, n.updatedAt)
        if err != nil {
            return err
        }
    }

    return rows.Err()
}

func (l *Lifter) liftEdges(ctx context.Context, tx *sql.Tx) error {
    // Only lift edges where both endpoints were lifted
    // Uses a JOIN against the already-lifted nodes in org.db
    rows, err := l.src.QueryContext(ctx, `
        SELECT e.id, e.source_id, e.target_id, e.type, e.source_class,
               e.plugin_id, e.properties, e.created_at, e.updated_at
        FROM edges e
        WHERE e.project_id = ?
          AND e.type IN ('imports', 'implements', 'extends',
                         'concept_of', 'depends_on')
    `, string(l.projectID))
    if err != nil {
        return err
    }
    defer rows.Close()

    // Build set of lifted node IDs for fast lookup
    liftedIDs, err := l.getLiftedNodeIDs(ctx, tx)
    if err != nil {
        return err
    }

    stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO edges (id, project_id, source_id, target_id,
                          type, source_class, plugin_id, properties,
                          created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            source_class = excluded.source_class,
            properties   = excluded.properties,
            updated_at   = excluded.updated_at
    `)
    if err != nil {
        return err
    }
    defer stmt.Close()

    for rows.Next() {
        var e dbEdge
        if err := rows.Scan(&e.id, &e.sourceID, &e.targetID, &e.edgeType,
            &e.sourceClass, &e.pluginID, &e.properties,
            &e.createdAt, &e.updatedAt); err != nil {
            return err
        }

        // Only lift if both endpoints are in the org graph
        if !liftedIDs[e.sourceID] || !liftedIDs[e.targetID] {
            continue
        }

        _, err := stmt.ExecContext(ctx,
            e.id, string(l.projectID), e.sourceID, e.targetID,
            e.edgeType, e.sourceClass, e.pluginID, e.properties,
            e.createdAt, e.updatedAt)
        if err != nil {
            return err
        }
    }

    return rows.Err()
}

func (l *Lifter) getLiftedNodeIDs(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
    rows, err := tx.QueryContext(ctx,
        `SELECT id FROM nodes WHERE project_id = ?`,
        string(l.projectID))
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    ids := make(map[string]bool)
    for rows.Next() {
        var id string
        if err := rows.Scan(&id); err != nil {
            return nil, err
        }
        ids[id] = true
    }
    return ids, rows.Err()
}
```

---

## 8. Cross-Project Edge Detection

After lifting, the org graph detects relationships that span project
boundaries. These become `cross_project_edges` with speculative source class.

```go
// internal/orggraph/crossproject.go

// DetectCrossProjectEdges finds relationships between nodes from different
// projects. Called after lifting completes for a project.
func (g *OrgGraph) DetectCrossProjectEdges(ctx context.Context, projectID core.ProjectID) error {
    // ── Strategy 1: Shared imports ─────────────────────────────────────────
    // If project A imports package X and project B imports package X,
    // they share a dependency. Connect them via the shared namespace node.
    if err := g.detectSharedDependencies(ctx, projectID); err != nil {
        return fmt.Errorf("shared dependencies: %w", err)
    }

    // ── Strategy 2: Matching interface names ───────────────────────────────
    // If project A defines interface Foo and project B defines interface Foo
    // with the same method signatures, they likely describe the same contract.
    if err := g.detectSharedInterfaces(ctx, projectID); err != nil {
        return fmt.Errorf("shared interfaces: %w", err)
    }

    // ── Strategy 3: Concept co-occurrence ─────────────────────────────────
    // If the same concept term appears in multiple projects, it represents
    // a shared domain concern. The org graph already captures this via
    // lifted concept nodes — no extra edges needed.

    return nil
}

func (g *OrgGraph) detectSharedDependencies(
    ctx       context.Context,
    projectID core.ProjectID,
) error {
    // Find namespace nodes in this project
    // Find other projects that also import these namespaces
    // Create cross-project edges: projectA_node → shared_namespace ← projectB_node
    // (The shared namespace node already exists — the edge points to it from both sides)

    _, err := g.db.ExecContext(ctx, `
        INSERT INTO cross_project_edges
            (id, source_node_id, source_project, target_node_id, target_project,
             type, source_class, weight, properties, created_at, updated_at)
        SELECT
            hex(randomblob(16)),
            e1.source_id,    -- namespace node in projectID
            e1.project_id,
            e2.source_id,    -- namespace node in other project
            e2.project_id,
            'shares_dependency',
            'speculative',
            0.3,
            json_object('shared_namespace', n.canonical_id),
            unixepoch() * 1000,
            unixepoch() * 1000
        FROM edges e1
        JOIN edges e2 ON e2.target_id = e1.target_id
                      AND e2.project_id != e1.project_id
        JOIN nodes n ON n.id = e1.target_id
        WHERE e1.project_id = ?
          AND e1.type = 'imports'
          AND e2.type = 'imports'
        ON CONFLICT DO NOTHING
    `, string(projectID))

    return err
}

func (g *OrgGraph) detectSharedInterfaces(
    ctx       context.Context,
    projectID core.ProjectID,
) error {
    // Find interface nodes in this project
    // Compare method signatures against interfaces in other projects
    // Create "mirrors" cross-project edges for matching interfaces

    interfaces, err := g.queries.GetInterfacesForProject(ctx, projectID)
    if err != nil {
        return err
    }

    for _, iface := range interfaces {
        methods := extractMethods(iface.Properties)
        if len(methods) == 0 {
            continue
        }

        // Find interfaces in other projects with same name AND same methods
        similar, err := g.queries.FindSimilarInterfaces(ctx, iface.Label, methods, projectID)
        if err != nil {
            return err
        }

        for _, match := range similar {
            edgeID := core.EdgeID(fmt.Sprintf("cross:%s→%s:mirrors", iface.ID, match.ID))
            _, err := g.db.ExecContext(ctx, `
                INSERT INTO cross_project_edges
                    (id, source_node_id, source_project, target_node_id, target_project,
                     type, source_class, weight, properties, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, 'mirrors', 'speculative', 0.5, ?, ?, ?)
                ON CONFLICT DO NOTHING
            `,
                string(edgeID),
                iface.ID, string(projectID),
                match.ID, string(match.ProjectID),
                fmt.Sprintf(`{"interface": "%s", "method_match": %d}`,
                    iface.Label, len(methods)),
                time.Now().UnixMilli(),
                time.Now().UnixMilli(),
            )
            if err != nil {
                return err
            }
        }
    }

    return nil
}
```

---

## 9. FindSimilar Query (crossproject tool)

```go
// internal/orggraph/queries/nodes.go

// FindSimilar is the core query used by the crossproject tool.
// Finds nodes in the org graph that match the given canonical ID
// and type, across all projects except the current one.
func (q *OrgQueries) FindSimilar(
    ctx         context.Context,
    canonicalID string,
    nodeType    string,
    limit       int,
    excludeProjectID core.ProjectID,
) ([]OrgMatch, error) {

    // Strategy 1: exact canonical ID match in other projects
    rows, err := q.db.QueryContext(ctx, `
        SELECT
            n.id, n.type, n.label, n.canonical_id,
            n.source_class, n.properties, n.project_id,
            m.name as project_name,
            1.0 as similarity
        FROM nodes n
        JOIN projects m ON m.id = n.project_id
        WHERE n.canonical_id = ?
          AND n.type = ?
          AND n.project_id != ?
        LIMIT ?
    `, canonicalID, nodeType, string(excludeProjectID), limit)

    // Strategy 2: suffix match (function name without package prefix)
    // e.g., "ProcessPayment" matches "other-service/billing:ProcessPayment"
    suffix := ":" + canonicalID
    if !strings.Contains(canonicalID, ":") {
        suffix = canonicalID
    }

    // ... combine results from both strategies

    return results, nil
}

type OrgMatch struct {
    Node        core.Node
    ProjectID   core.ProjectID
    ProjectName string
    Similarity  float64
}
```

---

## 10. Indexer Amendment — Lift After Index

The indexer's `Run()` method (Spec 9) is amended to call `Lift()` after
a successful index run.

```go
// internal/indexer/indexer.go (amended Run())

func (idx *Indexer) Run(ctx context.Context, projectID core.ProjectID, full bool) error {
    // ... existing indexer code ...

    // After flush, lift to org graph
    if err := idx.orgGraph.Lift(ctx, projectID, idx.projectDB); err != nil {
        // Non-fatal — log warning, project index is still good
        idx.channels.Emit(core.Emission{
            Channel: core.ChanWarning,
            Content: fmt.Sprintf("org graph lift: %v", err),
        })
    } else {
        // Detect cross-project relationships for newly lifted nodes
        if err := idx.orgGraph.DetectCrossProjectEdges(ctx, projectID); err != nil {
            idx.channels.Emit(core.Emission{
                Channel: core.ChanWarning,
                Content: fmt.Sprintf("cross-project edge detection: %v", err),
            })
        }
    }

    return nil
}
```

---

## 11. SubstrateReader Amendment — Org Graph Access

The crossproject tool (Spec 11) calls `FindInOrgGraph`. This method is
implemented in the substrate reader by delegating to the OrgGraph:

```go
// internal/graph/substrate/reader.go (amendment)

func (r *SubstrateReader) FindInOrgGraph(
    ctx         context.Context,
    canonicalID string,
    nodeType    string,
) ([]OrgMatch, error) {
    raw, err := r.orgGraph.FindSimilar(ctx, canonicalID, nodeType, 20, r.projectID)
    if err != nil {
        return nil, err
    }

    // Convert to core.OrgMatch
    result := make([]core.OrgMatch, len(raw))
    for i, m := range raw {
        result[i] = core.OrgMatch{
            Node:        m.Node,
            ProjectID:   m.ProjectID,
            ProjectName: m.ProjectName,
            Similarity:  m.Similarity,
        }
    }
    return result, nil
}
```

---

## 12. Org Concept Seeds

Org-level concept seeds are managed via `ce config` and stored in `org.db`.
They supplement per-project seeds and appear in all queries across all projects.

```go
// cli/config.go (additions)

// ce config org-concepts list
// ce config org-concepts add --term "event-sourcing" \
//   --definition "..." --related "cqrs,domain-events"
// ce config org-concepts remove --term "event-sourcing"

func runOrgConceptsList(cmd *cobra.Command, args []string) error {
    cfg, _ := config.Load()
    org, _ := orggraph.Open(cfg.DataDir)
    seeds, _ := org.GetOrgConceptSeeds(context.Background())

    for _, s := range seeds {
        fmt.Printf("%-30s %s\n", s.Term, s.Definition)
    }
    return nil
}
```

---

## 13. Performance Considerations

**Lifting cadence** — lifting runs after every index. For large orgs with
many projects indexing frequently, this could be expensive. Mitigation:
the lifter is idempotent and diff-based — it only UPSERTs changed nodes.
SQLite's `ON CONFLICT DO UPDATE` handles this naturally.

**Org graph size** — the org graph is smaller than the sum of all project
graphs because we only lift a subset of nodes. For an org with 20 projects,
expect 10-30% of each project's nodes to be lifted.

**Cross-project edge detection** — the shared dependency query is a SQL
JOIN that could be slow on large orgs. Index `edges.target_id` and
`edges.project_id` together. The `idx_org_edges_target` index covers this.

**Query latency** — `FindSimilar` runs two queries (exact + suffix). For
the crossproject tool, which runs during a live cognitive loop, both queries
must complete in <50ms. Validate with realistic data before shipping.

---

## 14. Package Layout Summary

```
internal/orggraph/
  orggraph.go         — OrgGraph, Open(), Lift(), FindSimilar(),
                        GetOrgConceptSeeds(), AddOrgConceptSeed()
  schema.go           — runMigrations(), org.db schema version management
  lift.go             — Lifter, liftNodes(), liftEdges(), getLiftedNodeIDs()
  crossproject.go     — DetectCrossProjectEdges(), detectSharedDependencies(),
                        detectSharedInterfaces()
  queries/
    nodes.go          — FindSimilar(), GetInterfacesForProject(),
                        FindSimilarInterfaces()
    edges.go          — cross-project edge queries
    concepts.go       — GetConceptSeeds(), UpsertConceptSeed()
```

---

## 15. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Org graph location | `~/.ce/org.db` — single file for all projects |
| Schema | Same as project graph.db — consistent query patterns |
| What gets lifted | Namespaces, exported symbols, interfaces, concepts — not files or internals |
| What gets lifted (edges) | imports, implements, extends, concept_of — not calls or defined_in |
| Lift trigger | After every successful index run |
| Lift idempotency | ON CONFLICT DO UPDATE — safe to re-run |
| Cross-project edges | Separate table (cross_project_edges) — keeps project edges clean |
| Cross-project detection | Shared imports + matching interface signatures |
| Initial cross-project weight | 0.3 (shared deps), 0.5 (matching interfaces) — speculative |
| FindSimilar strategies | Exact canonical ID match, then suffix match |
| Org concept seeds | Stored in org.db, managed via `ce config org-concepts` |
| Lift failures | Non-fatal — project index stands, warning emitted |
| Cross-project edge source class | Always speculative — humans confirm via Studio |
| Query latency target | <50ms for FindSimilar on realistic org sizes |

---

*Spec 17: Org Graph — v1.0 — February 2026*
*All seventeen specs complete. The system is fully specced.*
*Companion: Context Engine PRD v0.5 Section 12 | Decisions Log v1.0 Section 13*
