// Package writebuffer implements the single-writer goroutine for all substrate
// graph writes. Callers fire-and-forget — they never block waiting for write
// confirmation. The buffer deduplicates ops and flushes on two triggers:
// buffer full OR time elapsed.
package writebuffer

import "database/sql"

// OpType identifies the kind of write operation.
type OpType string

const (
	OpUpsertNode       OpType = "upsert_node"
	OpUpsertEdge       OpType = "upsert_edge"
	OpUpdateActivation OpType = "update_activation"
	OpUpdateWeight     OpType = "update_weight"
	OpUpsertConcept    OpType = "upsert_concept"
	OpUpsertIIR        OpType = "upsert_iir"
	OpRecordEnrichment OpType = "record_enrichment"
)

// WriteOp is the unit of work the buffer accepts.
type WriteOp struct {
	Type      OpType
	ProjectID string // determines which graph DB to write to
	Payload   any    // typed per OpType (see concrete types below)
}

// ActivationUpdate is the most frequent write.
// The buffer deduplicates: if node X has 5 pending activation updates,
// only the final value is written.
type ActivationUpdate struct {
	NodeID     string
	Activation float64
	UpdatedAt  int64
}

// WeightUpdate is an edge weight update from Hebbian learning.
// The buffer accumulates CoActivationDelta across pending ops for the same edge,
// but replaces Weight with the latest value.
type WeightUpdate struct {
	EdgeID            string
	Weight            float64
	SourceClass       string
	CoActivationDelta int // added to existing count, not replaced
	UpdatedAt         int64
}

// NodeUpsert inserts or updates a node row.
// Idempotent by design — same ID always produces the same result.
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

// EdgeUpsert inserts or updates an edge row.
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

// ConceptUpsert inserts or updates a concept seed row.
type ConceptUpsert struct {
	ID         string
	Term       string
	Scope      string // project | org
	Definition string
	Related    string // JSON array
	Synonyms   string // JSON array
	Source     string
	PluginID   string
	CreatedAt  int64
	UpdatedAt  int64
}

// IIRUpsert inserts or updates an IIR row (one per function node + kind).
// Idempotent by design — the same ID always produces the same result.
type IIRUpsert struct {
	ID         string
	ProjectID  string
	NodeID     string
	Kind       string // extracted | intended
	Language   string
	IIR        string // FunctionIntent JSON
	SourceHash string
	RunID      string
	CreatedAt  int64
	UpdatedAt  int64
}

// EnrichmentRecord is an enrichment entry. Never deduplicated — each is distinct.
type EnrichmentRecord struct {
	ID          string
	RunID       string
	TurnID      string
	LoopIndex   int
	EntityType  string
	EntityID    string
	Action      string
	BeforeState sql.NullString // NULL if no prior state
	AfterState  string         // JSON snapshot after change
	Rationale   sql.NullString
	CreatedAt   int64
}

// DBProvider resolves a project ID to its graph database.
// "org" returns the org graph. Any registered project ID returns that project's graph.
type DBProvider interface {
	GraphDB(projectID string) (*sql.DB, error)
}
