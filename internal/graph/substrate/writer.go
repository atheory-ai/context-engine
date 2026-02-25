package substrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/storage/writebuffer"
)

// Writer implements core.SubstrateWriter by forwarding all writes to the buffer.
// Fire-and-forget — callers never block on write confirmation.
type Writer struct {
	buffer writebuffer.Buffer
}

// NewWriter creates a Writer that sends all ops to the given buffer.
func NewWriter(buffer writebuffer.Buffer) *Writer {
	return &Writer{buffer: buffer}
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

// UpdateActivation queues an activation update for a node.
func (w *Writer) UpdateActivation(_ context.Context, nodeID core.NodeID, activation float64) error {
	// ProjectID is not needed for activation updates — the node_activation
	// table is in the same DB as the node. The buffer resolves DB by ProjectID,
	// but activation updates are sent with the node's project ID.
	// Callers should use UpdateActivationForProject when project ID is known.
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

// UpdateWeight queues an edge weight update.
func (w *Writer) UpdateWeight(_ context.Context, edgeID core.EdgeID, delta core.WeightDelta) error {
	return w.buffer.Send(writebuffer.WriteOp{
		Type:      writebuffer.OpUpdateWeight,
		ProjectID: "", // edge weight is global — project ID set by caller
		Payload: writebuffer.WeightUpdate{
			EdgeID:            string(edgeID),
			Weight:            delta.NewWeight,
			SourceClass:       string(delta.NewSourceClass),
			CoActivationDelta: delta.CoActivationDelta,
			UpdatedAt:         time.Now().UnixMilli(),
		},
	})
}

// RecordEnrichment queues an enrichment record (never deduplicated).
func (w *Writer) RecordEnrichment(_ context.Context, e core.Enrichment) error {
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
		Writer: NewWriter(buf),
	}
}

// ApplyEnrichment applies an approved enrichment to the substrate.
// Records the enrichment AND applies the structural change (if action requires it).
func (rw *ReadWriter) ApplyEnrichment(ctx context.Context, e core.Enrichment) error {
	switch e.Action {
	case "promoted":
		// Promoted means source_class upgraded (speculative → associative).
		// Apply via weight update with the new source class.
		if e.EntityType == "edge" {
			if err := rw.buffer.Send(writebuffer.WriteOp{
				Type:      writebuffer.OpUpdateWeight,
				ProjectID: "", // must be set by caller context — TODO: pass projectID
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
	return rw.RecordEnrichment(ctx, e)
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
