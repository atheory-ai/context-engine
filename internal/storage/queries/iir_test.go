package queries_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

func migratedGraph(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := migrations.RunGraph(d); err != nil {
		t.Fatalf("migrate graph: %v", err)
	}
	return d
}

func seedIIR(t *testing.T, d *sql.DB, projectID, nodeID, kind, payload string) {
	t.Helper()
	id := queries.IIRID(projectID, nodeID, kind)
	_, err := d.Exec(`
		INSERT INTO iir (id, project_id, node_id, kind, language, iir, source_hash, run_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'typescript', ?, 'h1', 'run1', 1, 1)
	`, id, projectID, nodeID, kind, payload)
	if err != nil {
		t.Fatalf("seed iir: %v", err)
	}
}

func TestIIRID_Deterministic(t *testing.T) {
	a := queries.IIRID("p", "n", "extracted")
	b := queries.IIRID("p", "n", "extracted")
	if a != b {
		t.Errorf("IIRID not deterministic: %q vs %q", a, b)
	}
	if a == queries.IIRID("p", "n", "intended") {
		t.Error("kind should change the id")
	}
}

func TestGetIIR_RoundTrip(t *testing.T) {
	d := migratedGraph(t)
	ctx := context.Background()
	seedIIR(t, d, "proj", "node-1", queries.IIRKindExtracted, `{"name":"f"}`)

	got, err := queries.GetIIR(ctx, d, "proj", "node-1", queries.IIRKindExtracted)
	if err != nil {
		t.Fatalf("GetIIR: %v", err)
	}
	if got == nil {
		t.Fatal("expected a row")
	}
	if got.Payload != `{"name":"f"}` || got.SourceHash != "h1" || got.RunID != "run1" {
		t.Errorf("unexpected row: %+v", got)
	}
}

func TestGetIIR_AbsentReturnsNil(t *testing.T) {
	d := migratedGraph(t)
	got, err := queries.GetIIR(context.Background(), d, "proj", "missing", queries.IIRKindExtracted)
	if err != nil {
		t.Fatalf("GetIIR: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for absent row, got %+v", got)
	}
}

func TestGetIIRByNode_BothKinds(t *testing.T) {
	d := migratedGraph(t)
	ctx := context.Background()
	seedIIR(t, d, "proj", "node-1", queries.IIRKindExtracted, `{"k":"e"}`)
	seedIIR(t, d, "proj", "node-1", queries.IIRKindIntended, `{"k":"i"}`)

	rows, err := queries.GetIIRByNode(ctx, d, "proj", "node-1")
	if err != nil {
		t.Fatalf("GetIIRByNode: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (both kinds), got %d", len(rows))
	}
	// kind-ordered: extracted before intended
	if rows[0].Kind != queries.IIRKindExtracted || rows[1].Kind != queries.IIRKindIntended {
		t.Errorf("unexpected kind order: %s, %s", rows[0].Kind, rows[1].Kind)
	}
}

func TestListIIRByProject_FiltersByKind(t *testing.T) {
	d := migratedGraph(t)
	ctx := context.Background()
	seedIIR(t, d, "proj", "node-1", queries.IIRKindExtracted, `{}`)
	seedIIR(t, d, "proj", "node-2", queries.IIRKindExtracted, `{}`)
	seedIIR(t, d, "proj", "node-1", queries.IIRKindIntended, `{}`)
	seedIIR(t, d, "other", "node-9", queries.IIRKindExtracted, `{}`)

	rows, err := queries.ListIIRByProject(ctx, d, "proj", queries.IIRKindExtracted)
	if err != nil {
		t.Fatalf("ListIIRByProject: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 extracted rows for proj, got %d", len(rows))
	}
	// node-id ordered
	if rows[0].NodeID != "node-1" || rows[1].NodeID != "node-2" {
		t.Errorf("unexpected order: %s, %s", rows[0].NodeID, rows[1].NodeID)
	}
}
