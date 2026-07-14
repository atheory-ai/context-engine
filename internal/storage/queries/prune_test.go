package queries_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

func insertNode(t *testing.T, d *sql.DB, projectID, id, typ, sourceFile string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, source_file, created_at, updated_at, properties)
		VALUES (?, ?, ?, ?, ?, 'structural', ?, 1, 1, '{}')
	`, id, projectID, typ, id, id, sourceFile)
	if err != nil {
		t.Fatalf("insert node %s: %v", id, err)
	}
}

func insertEdge(t *testing.T, d *sql.DB, projectID, id, src, tgt string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO edges (id, project_id, source_id, target_id, type, source_class, created_at, properties)
		VALUES (?, ?, ?, ?, 'calls', 'structural', 1, '{}')
	`, id, projectID, src, tgt)
	if err != nil {
		t.Fatalf("insert edge %s: %v", id, err)
	}
}

func insertActivation(t *testing.T, d *sql.DB, nodeID string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO node_activation (node_id, activation, peak_activation, updated_at) VALUES (?, 0.5, 0.5, 1)`, nodeID); err != nil {
		t.Fatalf("insert activation %s: %v", nodeID, err)
	}
}

func count(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func nodeExists(t *testing.T, d *sql.DB, id string) bool {
	return count(t, d, `SELECT COUNT(*) FROM nodes WHERE id = ?`, id) > 0
}

// TestPruneFileNodes_ChangedFile covers the incremental case: a changed file
// keeps the symbols it still emits (keepIDs) and drops the rest, with edges,
// activation, and IIR cascading/removed. Cross-file namespace nodes and other
// files' nodes are untouched.
func TestPruneFileNodes_ChangedFile(t *testing.T) {
	d := migratedGraph(t)
	ctx := context.Background()
	const proj = "p"

	insertNode(t, d, proj, "survivor", "symbol", "a.go") // still emitted
	insertNode(t, d, proj, "removed", "symbol", "a.go")  // dropped this run
	insertNode(t, d, proj, "ns", "namespace", "a.go")    // cross-file — must be skipped
	insertNode(t, d, proj, "other", "symbol", "b.go")    // different file — untouched
	insertEdge(t, d, proj, "e1", "survivor", "removed")  // inbound to removed → cascades
	insertActivation(t, d, "removed")
	seedIIR(t, d, proj, "removed", queries.IIRKindExtracted, `{"name":"removed"}`)
	seedIIR(t, d, proj, "survivor", queries.IIRKindExtracted, `{"name":"survivor"}`)
	if _, err := d.Exec(`INSERT INTO semantic_plans (id, project_id, unit_id, unit_node_id, revision, lifecycle, schema_version, payload, created_at) VALUES ('plan', ?, 'unit', 'removed', 1, 'resolved', 'v1', '{}', 1)`, proj); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO semantic_recipes (id, project_id, plan_revision_id, schema_version, target_language, renderer_profile, payload, created_at) VALUES ('recipe', ?, 'plan', 'v1', 'typescript', '{}', '{}', 1)`, proj); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO semantic_artifacts (id, project_id, plan_revision_id, recipe_id, unit_node_id, kind, content_hash, target_language, target_path, created_at) VALUES ('artifact', ?, 'plan', 'recipe', 'removed', 'source', 'hash', 'typescript', 'a.ts', 1)`, proj); err != nil {
		t.Fatal(err)
	}

	n, err := queries.NewIndexQueries(d).PruneFileNodes(ctx, proj, "a.go", []string{"survivor", "ns"})
	if err != nil {
		t.Fatalf("PruneFileNodes: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d nodes, want 1 (only 'removed')", n)
	}

	if nodeExists(t, d, "removed") {
		t.Error("'removed' should be gone")
	}
	if !nodeExists(t, d, "survivor") {
		t.Error("'survivor' (in keepIDs) must be kept")
	}
	if !nodeExists(t, d, "ns") {
		t.Error("namespace node must be skipped by the prune")
	}
	if !nodeExists(t, d, "other") {
		t.Error("other file's node must be untouched")
	}
	// Cascades + explicit IIR delete for the removed node.
	if c := count(t, d, `SELECT COUNT(*) FROM edges WHERE id = 'e1'`); c != 0 {
		t.Errorf("edge to removed node should cascade-delete, found %d", c)
	}
	if c := count(t, d, `SELECT COUNT(*) FROM node_activation WHERE node_id = 'removed'`); c != 0 {
		t.Errorf("activation for removed node should cascade-delete, found %d", c)
	}
	if c := count(t, d, `SELECT COUNT(*) FROM iir WHERE node_id = 'removed'`); c != 0 {
		t.Errorf("iir for removed node should be deleted, found %d", c)
	}
	if c := count(t, d, `SELECT COUNT(*) FROM iir WHERE node_id = 'survivor'`); c != 1 {
		t.Errorf("iir for survivor must be kept, found %d", c)
	}
	var staleAt int64
	var unitNodeID sql.NullString
	if err := d.QueryRow(`SELECT COALESCE(stale_at, 0), unit_node_id FROM semantic_artifacts WHERE id = 'artifact'`).Scan(&staleAt, &unitNodeID); err != nil {
		t.Fatal(err)
	}
	if staleAt == 0 || unitNodeID.Valid {
		t.Fatalf("artifact should be stale and detached after prune: staleAt=%d unitNode=%+v", staleAt, unitNodeID)
	}
}

// TestPruneFileNodes_DeletedFile covers a file removed from disk: an empty
// keepIDs removes every file-local node it contributed (namespace still skipped).
func TestPruneFileNodes_DeletedFile(t *testing.T) {
	d := migratedGraph(t)
	ctx := context.Background()
	const proj = "p"

	insertNode(t, d, proj, "f1", "symbol", "gone.go")
	insertNode(t, d, proj, "f2", "symbol", "gone.go")
	insertNode(t, d, proj, "keep", "symbol", "stays.go")

	n, err := queries.NewIndexQueries(d).PruneFileNodes(ctx, proj, "gone.go", nil)
	if err != nil {
		t.Fatalf("PruneFileNodes: %v", err)
	}
	if n != 2 {
		t.Errorf("pruned %d nodes, want 2", n)
	}
	if nodeExists(t, d, "f1") || nodeExists(t, d, "f2") {
		t.Error("deleted file's nodes should be gone")
	}
	if !nodeExists(t, d, "keep") {
		t.Error("other file's node must survive")
	}
}
