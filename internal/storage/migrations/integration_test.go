package migrations_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestMigrationSchemasGolden(t *testing.T) {
	tmp := t.TempDir()

	metaDB := openMigrated(t, filepath.Join(tmp, "meta.db"), migrations.RunMeta)
	auditDB := openMigrated(t, filepath.Join(tmp, "audit.db"), migrations.RunAudit)
	execDB := openMigrated(t, filepath.Join(tmp, "execution.db"), migrations.RunExecution)
	graphDB := openMigrated(t, filepath.Join(tmp, "graph.db"), migrations.RunGraph)
	orgDB := openMigrated(t, filepath.Join(tmp, "org.db"), func(db *sql.DB) error {
		if err := migrations.RunGraph(db); err != nil {
			return err
		}
		return migrations.RunOrg(db)
	})

	out := map[string]any{
		"meta":      schemaSummary(t, metaDB),
		"audit":     schemaSummary(t, auditDB),
		"execution": schemaSummary(t, execDB),
		"graph":     schemaSummary(t, graphDB),
		"org":       schemaSummary(t, orgDB),
		"isolation": map[string]any{
			"meta_has_tokens":             tableExists(t, metaDB, "tokens"),
			"meta_has_audit_entries":      tableExists(t, metaDB, "audit_entries"),
			"meta_has_execution_log":      tableExists(t, metaDB, "execution_log"),
			"audit_has_tokens":            tableExists(t, auditDB, "tokens"),
			"audit_has_audit_entries":     tableExists(t, auditDB, "audit_entries"),
			"audit_has_execution_log":     tableExists(t, auditDB, "execution_log"),
			"execution_has_tokens":        tableExists(t, execDB, "tokens"),
			"execution_has_audit_entries": tableExists(t, execDB, "audit_entries"),
			"execution_has_execution_log": tableExists(t, execDB, "execution_log"),
			"graph_has_nodes":             tableExists(t, graphDB, "nodes"),
			"graph_has_audit_entries":     tableExists(t, graphDB, "audit_entries"),
			"graph_has_execution_log":     tableExists(t, graphDB, "execution_log"),
			"org_has_graph_nodes":         tableExists(t, orgDB, "nodes"),
			"org_has_org_concepts":        tableExists(t, orgDB, "org_concept_seeds"),
			"org_has_cross_project_edges": tableExists(t, orgDB, "cross_project_edges"),
		},
	}

	assertGolden(t, "schemas", out)
}

func TestSemanticBuildGraphMigrationConstraintsAndRollback(t *testing.T) {
	database := openMigrated(t, filepath.Join(t.TempDir(), "graph.db"), migrations.RunGraph)
	if !tableExists(t, database, "semantic_plans") {
		t.Fatal("semantic plans migration did not apply")
	}
	if _, err := database.Exec(`INSERT INTO semantic_plans (id, project_id, unit_id, revision, lifecycle, schema_version, payload, created_at) VALUES ('bad-schema', 'p', 'u', 1, 'resolved', 'v2', '{}', 1)`); err == nil {
		t.Fatal("semantic plan schema-version check accepted v2")
	}
	if _, err := database.Exec(`INSERT INTO semantic_recipes (id, project_id, plan_revision_id, schema_version, target_language, renderer_profile, payload, created_at) VALUES ('orphan', 'p', 'missing', 'v1', 'typescript', '{}', '{}', 1)`); err == nil {
		t.Fatal("semantic recipe foreign key accepted missing plan")
	}
	// Index staging, ownership, and durable artifacts follow the semantic graph
	// migration; roll all four back to verify it remains reversible.
	if err := migrations.RollbackGraph(database, 4); err != nil {
		t.Fatalf("rollback semantic build graph: %v", err)
	}
	if tableExists(t, database, "semantic_plans") {
		t.Fatal("semantic plans table remained after rollback")
	}
	if err := migrations.RunGraph(database); err != nil {
		t.Fatalf("reapply semantic build graph: %v", err)
	}
	if !tableExists(t, database, "semantic_plans") {
		t.Fatal("semantic plans table absent after reapply")
	}
}

func openMigrated(t *testing.T, path string, migrate func(*sql.DB) error) *sql.DB {
	t.Helper()
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { database.Close() })
	if err := migrate(database); err != nil {
		t.Fatalf("migrate %s: %v", path, err)
	}
	return database
}

func schemaSummary(t *testing.T, database *sql.DB) map[string][]string {
	t.Helper()
	rows, err := database.Query(`
		SELECT type, name
		FROM sqlite_schema
		WHERE type IN ('table', 'index') AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name
	`)
	if err != nil {
		t.Fatalf("query sqlite_schema: %v", err)
	}
	defer rows.Close()

	out := map[string][]string{
		"index": {},
		"table": {},
	}
	for rows.Next() {
		var typ, name string
		if err := rows.Scan(&typ, &name); err != nil {
			t.Fatalf("scan sqlite_schema: %v", err)
		}
		out[typ] = append(out[typ], name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sqlite_schema: %v", err)
	}
	return out
}

func tableExists(t *testing.T, database *sql.DB, name string) bool {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return count == 1
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
