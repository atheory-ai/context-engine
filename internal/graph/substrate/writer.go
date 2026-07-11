package substrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

// Writer implements core.SubstrateWriter by forwarding all writes to the buffer.
// For operations that require direct SQL (DecayEdgeWeights, ResetActivation),
// it uses the dbProvider to access the graph database directly.
// Fire-and-forget — callers never block on write confirmation.
type Writer struct {
	buffer     writebuffer.Buffer
	dbProvider writebuffer.DBProvider
}

// NewWriter creates a Writer that sends all ops to the given buffer.
func NewWriter(buffer writebuffer.Buffer, dbProvider writebuffer.DBProvider) *Writer {
	return &Writer{buffer: buffer, dbProvider: dbProvider}
}

// UpsertNode queues a node insert/update via the write buffer.
func (w *Writer) UpsertNode(_ context.Context, node core.Node) error {
	props, err := marshalProperties(node.Properties)
	if err != nil {
		return fmt.Errorf("marshal node properties: %w", err)
	}
	return w.buffer.Send(writebuffer.WriteOp{
		Type:      writebuffer.OpUpsertNode,
		ProjectID: string(node.ProjectID),
		Payload: writebuffer.NodeUpsert{
			ID:          string(node.ID),
			ProjectID:   string(node.ProjectID),
			Type:        node.Type,
			Label:       node.Label,
			CanonicalID: node.CanonicalID,
			SourceClass: string(node.SourceClass),
			PluginID:    string(node.PluginID),
			SourceFile:  node.SourceFile,
			Properties:  props,
			CreatedAt:   node.CreatedAt,
			UpdatedAt:   node.UpdatedAt,
		},
	})
}

// UpsertEdge queues an edge insert/update via the write buffer.
func (w *Writer) UpsertEdge(_ context.Context, edge core.Edge) error {
	props, err := marshalProperties(edge.Properties)
	if err != nil {
		return fmt.Errorf("marshal edge properties: %w", err)
	}
	return w.buffer.Send(writebuffer.WriteOp{
		Type:      writebuffer.OpUpsertEdge,
		ProjectID: string(edge.ProjectID),
		Payload: writebuffer.EdgeUpsert{
			ID:          string(edge.ID),
			ProjectID:   string(edge.ProjectID),
			SourceID:    string(edge.SourceID),
			TargetID:    string(edge.TargetID),
			Type:        edge.Type,
			SourceClass: string(edge.SourceClass),
			PluginID:    string(edge.PluginID),
			Properties:  props,
			CreatedAt:   edge.CreatedAt,
		},
	})
}

// UpsertIIR queues an IIR insert/update via the write buffer. The row id is
// derived deterministically from (project, node, kind) so re-extraction upserts
// in place.
func (w *Writer) UpsertIIR(_ context.Context, r core.IIRRecord) error {
	return w.buffer.Send(writebuffer.WriteOp{
		Type:      writebuffer.OpUpsertIIR,
		ProjectID: string(r.ProjectID),
		Payload: writebuffer.IIRUpsert{
			ID:         queries.IIRID(string(r.ProjectID), string(r.NodeID), r.Kind),
			ProjectID:  string(r.ProjectID),
			NodeID:     string(r.NodeID),
			Kind:       r.Kind,
			Language:   r.Language,
			IIR:        r.Payload,
			SourceHash: r.SourceHash,
			RunID:      r.RunID,
			CreatedAt:  r.CreatedAt,
			UpdatedAt:  r.UpdatedAt,
		},
	})
}

// UpdateActivation queues an activation update for a node.
func (w *Writer) UpdateActivation(_ context.Context, nodeID core.NodeID, activation float64) error {
	return w.buffer.Send(writebuffer.WriteOp{
		Type:      writebuffer.OpUpdateActivation,
		ProjectID: "", // resolved by the caller via UpdateActivationForProject
		Payload: writebuffer.ActivationUpdate{
			NodeID:     string(nodeID),
			Activation: activation,
			UpdatedAt:  time.Now().UnixMilli(),
		},
	})
}

// UpdateActivationForProject queues an activation update with an explicit project ID.
// This is the preferred method — it ensures the buffer routes to the correct DB.
func (w *Writer) UpdateActivationForProject(_ context.Context, projectID core.ProjectID, nodeID core.NodeID, activation float64) error {
	return w.buffer.Send(writebuffer.WriteOp{
		Type:      writebuffer.OpUpdateActivation,
		ProjectID: string(projectID),
		Payload: writebuffer.ActivationUpdate{
			NodeID:     string(nodeID),
			Activation: activation,
			UpdatedAt:  time.Now().UnixMilli(),
		},
	})
}

// UpdateEdgeWeight queues an edge weight update from Hebbian learning.
func (w *Writer) UpdateEdgeWeight(_ context.Context, update core.WeightUpdate) error {
	return w.buffer.Send(writebuffer.WriteOp{
		Type:      writebuffer.OpUpdateWeight,
		ProjectID: string(update.ProjectID),
		Payload: writebuffer.WeightUpdate{
			EdgeID:            string(update.EdgeID),
			Weight:            update.NewWeight,
			SourceClass:       update.SourceClass,
			CoActivationDelta: update.CoActivationDelta,
			UpdatedAt:         time.Now().UnixMilli(),
		},
	})
}

// DecayEdgeWeights reduces all edge weights for a project by decayRate.
// Runs as a single SQL UPDATE — bypasses write buffer for atomicity.
func (w *Writer) DecayEdgeWeights(ctx context.Context, projectID core.ProjectID, decayRate float64) error {
	db, err := w.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.DecayEdgeWeightsSQL(ctx, db, string(projectID), decayRate, time.Now().UnixMilli())
}

// ResetActivation zeroes all activation values for a project.
// Called at the start of each new query to prevent cross-query bleed.
// Runs as a single SQL UPDATE — bypasses write buffer.
func (w *Writer) ResetActivation(ctx context.Context, projectID core.ProjectID) error {
	db, err := w.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.ResetActivationSQL(ctx, db, string(projectID), time.Now().UnixMilli())
}

// ApplyEnrichment applies an approved enrichment to the substrate.
// Records the enrichment AND applies the structural change (if action requires it).
func (w *Writer) ApplyEnrichment(ctx context.Context, e core.Enrichment) error {
	switch e.Action {
	case "promoted":
		// Promoted means source_class upgraded (speculative → associative).
		// Apply via weight update with the new source class.
		if e.EntityType == "edge" {
			if err := w.buffer.Send(writebuffer.WriteOp{
				Type:      writebuffer.OpUpdateWeight,
				ProjectID: "", // best-effort — project ID not available here
				Payload: writebuffer.WeightUpdate{
					EdgeID:      e.EntityID,
					SourceClass: string(core.SourceAssociative),
					UpdatedAt:   time.Now().UnixMilli(),
				},
			}); err != nil {
				return err
			}
		}
	case "created", "updated":
		// Structural changes are handled by the Reviewer writing nodes/edges directly.
	}

	// Always record the enrichment provenance.
	return w.recordEnrichment(ctx, e)
}

// recordEnrichment queues an enrichment record (never deduplicated).
func (w *Writer) recordEnrichment(_ context.Context, e core.Enrichment) error {
	afterJSON, err := json.Marshal(e.AfterState)
	if err != nil {
		return fmt.Errorf("marshal enrichment after state: %w", err)
	}
	var beforeJSON string
	if e.BeforeState != nil {
		b, err := json.Marshal(e.BeforeState)
		if err != nil {
			return fmt.Errorf("marshal enrichment before state: %w", err)
		}
		beforeJSON = string(b)
	}
	return w.buffer.Send(writebuffer.WriteOp{
		Type:      writebuffer.OpRecordEnrichment,
		ProjectID: string(e.RunID), // enrichments stored in project graph
		Payload: writebuffer.EnrichmentRecord{
			RunID:       string(e.RunID),
			TurnID:      string(e.TurnID),
			LoopIndex:   e.LoopIndex,
			EntityType:  e.EntityType,
			EntityID:    e.EntityID,
			Action:      e.Action,
			BeforeState: nullString(beforeJSON),
			AfterState:  string(afterJSON),
			CreatedAt:   time.Now().UnixMilli(),
		},
	})
}

// Flush forces all pending writes to be committed to the database.
// Called by the indexer after a run is complete.
func (w *Writer) Flush(ctx context.Context) error {
	return w.buffer.Flush(ctx)
}

// ============================================================
// ReadWriter — combines Reader and Writer
// ============================================================

// ReadWriter implements both core.SubstrateReader and core.SubstrateWriter.
// It is the primary substrate access object used throughout the engine.
// All reads are direct SQL queries; all writes go through the write buffer.
type ReadWriter struct {
	*Reader
	*Writer
}

// NewReadWriter creates a ReadWriter backed by the given DBProvider and write buffer.
func NewReadWriter(dbProvider writebuffer.DBProvider, buf writebuffer.Buffer) *ReadWriter {
	return &ReadWriter{
		Reader: NewReader(dbProvider),
		Writer: NewWriter(buf, dbProvider),
	}
}

// ============================================================
// Helpers
// ============================================================

func marshalProperties(props map[string]any) (string, error) {
	if len(props) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(props)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// nullString returns a sql.NullString from a Go string.
// Blank strings become NULL.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
