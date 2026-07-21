package queries_test

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

func TestReconcileIndexRun_FullReplacesMovedFactsAndLegacyOutput(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)
	const project = "p"

	// This is the shape produced by the old indexer: a plugin fact whose ID
	// changed when its source offset moved, plus an edge and IIR tied to it.
	for _, id := range []string{"old-symbol", "new-symbol"} {
		run := "old-run"
		if id == "new-symbol" {
			run = "new-run"
		}
		if _, err := database.Exec(`INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, plugin_id, index_managed, last_index_run_id, created_at, updated_at, properties) VALUES (?, ?, 'symbol', ?, ?, 'structural', 'plugin', 1, ?, 1, 1, '{}')`, id, project, id, id, run); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO edges (id, project_id, source_id, target_id, type, source_class, plugin_id, index_managed, last_index_run_id, created_at, properties) VALUES ('old-edge', ?, 'old-symbol', 'new-symbol', 'calls', 'structural', 'plugin', 1, 'old-run', 1, '{}')`, project); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO iir (id, project_id, node_id, kind, language, iir, run_id, created_at, updated_at) VALUES ('old-iir', ?, 'old-symbol', 'extracted', 'go', '{}', 'old-run', 1, 1)`, project); err != nil {
		t.Fatal(err)
	}
	if err := q.StartIndexRun(ctx, "new-run", project, []string{"plugin"}, 2); err != nil {
		t.Fatal(err)
	}

	err := q.ReconcileIndexRun(ctx, project, "new-run", map[string]queries.FileOutput{
		"changed.go": {Hash: "new-hash", NodeIDs: []string{"new-symbol"}},
	}, map[string]struct{}{"changed.go": {}}, true, 1, 1, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if nodeExists(t, database, "old-symbol") || count(t, database, `SELECT COUNT(*) FROM edges WHERE id = 'old-edge'`) != 0 || count(t, database, `SELECT COUNT(*) FROM iir WHERE id = 'old-iir'`) != 0 {
		t.Fatal("old source-offset output survived authoritative full reindex")
	}
	if !nodeExists(t, database, "new-symbol") {
		t.Fatal("fresh output was deleted")
	}
	hashes, err := q.GetFileHashes(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 1 || hashes["changed.go"] != "new-hash" {
		t.Fatalf("hashes = %#v", hashes)
	}
}

func TestReconcileIndexRun_IncrementalRemovesStaleEdgeWithSurvivingEndpoints(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)
	const project, oldRun, newRun = "p", "old-run", "new-run"
	for _, id := range []string{"left", "right"} {
		if _, err := database.Exec(`INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, plugin_id, index_managed, last_index_run_id, created_at, updated_at, properties) VALUES (?, ?, 'symbol', ?, ?, 'structural', 'plugin', 1, ?, 1, 1, '{}')`, id, project, id, id, oldRun); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO edges (id, project_id, source_id, target_id, type, source_class, plugin_id, index_managed, last_index_run_id, created_at, properties) VALUES ('stale-edge', ?, 'left', 'right', 'calls', 'structural', 'plugin', 1, ?, 1, '{}')`, project, oldRun); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO index_file_nodes (project_id, rel_path, node_id, last_seen_run_id) VALUES ('p', 'left.go', 'left', 'old-run')`,
		`INSERT INTO index_file_nodes (project_id, rel_path, node_id, last_seen_run_id) VALUES ('p', 'right.go', 'right', 'old-run')`,
		`INSERT INTO index_file_edges (project_id, rel_path, edge_id, last_seen_run_id) VALUES ('p', 'changed.go', 'stale-edge', 'old-run')`,
		`INSERT INTO file_hashes (project_id, rel_path, hash, indexed_at) VALUES ('p', 'changed.go', 'old', 1)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.StartIndexRun(ctx, newRun, project, nil, 2); err != nil {
		t.Fatal(err)
	}
	if err := q.ReconcileIndexRun(ctx, project, newRun, map[string]queries.FileOutput{"changed.go": {Hash: "new"}}, map[string]struct{}{"changed.go": {}, "left.go": {}, "right.go": {}}, false, 1, 0, 0, 3); err != nil {
		t.Fatal(err)
	}
	if count(t, database, `SELECT COUNT(*) FROM edges WHERE id = 'stale-edge'`) != 0 {
		t.Fatal("stale edge survived although both endpoints remain")
	}
	if !nodeExists(t, database, "left") || !nodeExists(t, database, "right") {
		t.Fatal("surviving endpoints were removed")
	}
}

func TestReconcileIndexRun_PromotesStagedOutputWithoutChangingNullProvenance(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)
	const project, run = "p", "run"
	if err := q.StartIndexRun(ctx, run, project, nil, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"left", "right"} {
		if _, err := database.Exec(`INSERT INTO index_staging_nodes (run_id, id, project_id, type, label, canonical_id, source_class, plugin_id, source_file, index_managed, last_index_run_id, created_at, updated_at, properties) VALUES (?, ?, ?, 'symbol', ?, ?, 'structural', NULL, '', 1, ?, 1, 1, '{}')`, run, id, project, id, id, run); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO index_staging_edges (run_id, id, project_id, source_id, target_id, type, source_class, plugin_id, index_managed, last_index_run_id, created_at, properties) VALUES (?, 'edge', ?, 'left', 'right', 'calls', 'structural', NULL, 1, ?, 1, '{}')`, run, project, run); err != nil {
		t.Fatal(err)
	}
	if err := q.ReconcileIndexRun(ctx, project, run, map[string]queries.FileOutput{"file.go": {Hash: "hash", NodeIDs: []string{"left", "right"}, EdgeIDs: []string{"edge"}}}, map[string]struct{}{"file.go": {}}, true, 1, 2, 1, 2); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT plugin_id IS NULL AND last_index_run_id = 'run' FROM nodes WHERE id = 'left'`,
		`SELECT plugin_id IS NULL AND last_index_run_id = 'run' FROM edges WHERE id = 'edge'`,
	} {
		if count(t, database, query) != 1 {
			t.Fatalf("nullable provenance was not preserved: %s", query)
		}
	}
}

func TestFailIndexRunClearsAllDurableStaging(t *testing.T) {
	database := migratedGraph(t)
	q := queries.NewIndexQueries(database)
	ctx := context.Background()
	const run, project = "cancelled", "p"
	if err := q.StartIndexRun(ctx, run, project, nil, 1); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO index_staging_files (run_id,project_id,rel_path,source_hash,status) VALUES ('cancelled','p','a.go','h','indexed')`,
		`INSERT INTO index_staging_file_nodes (run_id,rel_path,node_id) VALUES ('cancelled','a.go','n')`,
		`INSERT INTO index_staging_file_edges (run_id,rel_path,edge_id) VALUES ('cancelled','a.go','e')`,
		`INSERT INTO index_staging_file_iir (run_id,rel_path,iir_id) VALUES ('cancelled','a.go','i')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.FailIndexRun(ctx, run, 2, context.Canceled); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"index_staging_files", "index_staging_file_nodes", "index_staging_file_edges", "index_staging_file_iir"} {
		if got := count(t, database, `SELECT COUNT(*) FROM `+table+` WHERE run_id='cancelled'`); got != 0 {
			t.Fatalf("%s retained %d rows", table, got)
		}
	}
	if got := count(t, database, `SELECT COUNT(*) FROM index_runs WHERE id='cancelled' AND status='failed'`); got != 1 {
		t.Fatalf("run status = %d", got)
	}
}

func TestParseArtifactRoundTrip(t *testing.T) {
	database := migratedGraph(t)
	q := queries.NewIndexQueries(database)
	if err := q.StoreParseArtifact(context.Background(), "p", "h", "v1", ".go", []byte("package p"), []byte(`{"root":{}}`), 1); err != nil {
		t.Fatal(err)
	}
	source, cst, err := q.ParseArtifact(context.Background(), "p", "h", "v1", ".go")
	if err != nil {
		t.Fatal(err)
	}
	if string(source) != "package p" || string(cst) != `{"root":{}}` {
		t.Fatalf("artifact = %q / %q", source, cst)
	}
}
