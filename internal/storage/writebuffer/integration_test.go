package writebuffer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestFlushWritesGolden(t *testing.T) {
	graphDB := setupGraphDB(t)
	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)

	now := int64(1700000000000)
	nodeOps := []writebuffer.WriteOp{
		{
			Type:      writebuffer.OpUpsertNode,
			ProjectID: "proj-golden",
			Payload: writebuffer.NodeUpsert{
				ID: "node-a", ProjectID: "proj-golden", Type: "symbol", Label: "Alpha",
				CanonicalID: "pkg.Alpha", SourceClass: "structural", Properties: `{"file":"alpha.go"}`,
				CreatedAt: now, UpdatedAt: now,
			},
		},
		{
			Type:      writebuffer.OpUpsertNode,
			ProjectID: "proj-golden",
			Payload: writebuffer.NodeUpsert{
				ID: "node-b", ProjectID: "proj-golden", Type: "symbol", Label: "Beta",
				CanonicalID: "pkg.Beta", SourceClass: "structural", Properties: `{}`,
				CreatedAt: now, UpdatedAt: now,
			},
		},
	}
	for _, op := range nodeOps {
		if err := buf.Send(op); err != nil {
			t.Fatalf("send %s: %v", op.Type, err)
		}
	}
	if err := buf.Flush(ctx); err != nil {
		t.Fatalf("flush nodes: %v", err)
	}

	dependentOps := []writebuffer.WriteOp{
		{
			Type:      writebuffer.OpUpsertEdge,
			ProjectID: "proj-golden",
			Payload: writebuffer.EdgeUpsert{
				ID: "edge-a-b", ProjectID: "proj-golden", SourceID: "node-a", TargetID: "node-b",
				Type: "calls", SourceClass: "structural", Properties: `{}`, CreatedAt: now,
			},
		},
		{
			Type:      writebuffer.OpUpdateActivation,
			ProjectID: "proj-golden",
			Payload:   writebuffer.ActivationUpdate{NodeID: "node-a", Activation: 0.8, UpdatedAt: now + 1},
		},
		{
			Type:      writebuffer.OpUpdateWeight,
			ProjectID: "proj-golden",
			Payload:   writebuffer.WeightUpdate{EdgeID: "edge-a-b", Weight: 0.9, SourceClass: "associative", CoActivationDelta: 2, UpdatedAt: now + 2},
		},
		{
			Type:      writebuffer.OpUpsertConcept,
			ProjectID: "proj-golden",
			Payload: writebuffer.ConceptUpsert{
				ID: "concept-alpha", Term: "activation", Scope: "project", Definition: "Node relevance score",
				Related: `["hebbian"]`, Synonyms: `["score"]`, Source: "test", CreatedAt: now, UpdatedAt: now,
			},
		},
		{
			Type:      writebuffer.OpRecordEnrichment,
			ProjectID: "proj-golden",
			Payload: writebuffer.EnrichmentRecord{
				ID: "enrichment-1", RunID: "run-1", TurnID: "turn-1", LoopIndex: 1,
				EntityType: "node", EntityID: "node-a", Action: "updated", AfterState: `{"label":"Alpha"}`, CreatedAt: now + 3,
			},
		},
	}

	for _, op := range dependentOps {
		if err := buf.Send(op); err != nil {
			t.Fatalf("send %s: %v", op.Type, err)
		}
	}
	if err := buf.Flush(ctx); err != nil {
		t.Fatalf("flush dependent writes: %v", err)
	}

	out := map[string]any{
		"nodes":       queryRows(t, graphDB, `SELECT id, project_id, type, label, canonical_id, source_class, properties FROM nodes ORDER BY id`),
		"edges":       queryRows(t, graphDB, `SELECT id, project_id, source_id, target_id, type, source_class, properties FROM edges ORDER BY id`),
		"activation":  queryRows(t, graphDB, `SELECT node_id, activation, peak_activation, updated_at FROM node_activation ORDER BY node_id`),
		"weights":     queryRows(t, graphDB, `SELECT edge_id, weight, source_class, co_activation_count, last_co_activation, updated_at FROM edge_weight ORDER BY edge_id`),
		"concepts":    queryRows(t, graphDB, `SELECT id, term, scope, definition, related, synonyms, source FROM concept_seeds ORDER BY id`),
		"enrichments": queryRows(t, graphDB, `SELECT id, run_id, turn_id, loop_index, entity_type, entity_id, action, after_state, created_at FROM enrichments ORDER BY id`),
	}

	assertGolden(t, "flush_writes", out)
}

func assertGolden(t *testing.T, name string, value any) {
	t.Helper()
	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden value: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", name+".golden.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}
