package queries_test

import (
	"context"
	"strings"
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
	if err := q.StartIndexRun(ctx, "new-run", project, []string{"plugin"}, "fingerprint", 2); err != nil {
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
	if err := q.StartIndexRun(ctx, newRun, project, nil, "fingerprint", 2); err != nil {
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
	if err := q.StartIndexRun(ctx, run, project, nil, "fingerprint", 1); err != nil {
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

func TestReconcileStagedIndexRunReportsBrokenEdgeEndpoint(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)
	const project, run = "p", "broken-edge-run"
	if err := q.StartIndexRun(ctx, run, project, nil, "fingerprint", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO index_staging_nodes (run_id,id,project_id,type,label,canonical_id,source_class,plugin_id,source_file,index_managed,last_index_run_id,created_at,updated_at,properties) VALUES (?, 'present', ?, 'symbol', 'present', 'present', 'structural', NULL, '', 1, ?, 1, 1, '{}')`, run, project, run); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO index_staging_edges (run_id,id,project_id,source_id,target_id,type,source_class,plugin_id,index_managed,last_index_run_id,created_at,properties) VALUES (?, 'broken', ?, 'present', 'missing', 'calls', 'structural', 'com.example.plugin', 1, ?, 1, '{}')`, run, project, run); err != nil {
		t.Fatal(err)
	}
	err := q.ReconcileStagedIndexRun(ctx, project, run, true, 1, 1, 1, 2)
	if err == nil || !strings.Contains(err.Error(), `staged edge broken from plugin "com.example.plugin" has missing endpoint(s): target=missing`) {
		t.Fatalf("ReconcileStagedIndexRun() error = %v", err)
	}
	if count(t, database, `SELECT COUNT(*) FROM nodes`) != 0 {
		t.Fatal("failed reconciliation published nodes")
	}
}

func TestFailIndexRunClearsAllDurableStaging(t *testing.T) {
	database := migratedGraph(t)
	q := queries.NewIndexQueries(database)
	ctx := context.Background()
	const run, project = "cancelled", "p"
	if err := q.StartIndexRun(ctx, run, project, nil, "fingerprint", 1); err != nil {
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

func TestStageFileEventsBatchesDiscoveryAndOutputAtomically(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)
	const run, project = "run", "p"
	if err := q.StartIndexRun(ctx, run, project, nil, "fingerprint", 1); err != nil {
		t.Fatal(err)
	}
	output := queries.FileOutput{Hash: "hash", NodeIDs: []string{"node", "node"}, EdgeIDs: []string{"edge"}, IIRIDs: []string{"iir"}}
	if err := q.StageFileEvents(ctx, run, project, []queries.StagedFileEvent{
		{Path: "walked.go"},
		{Path: "indexed.go", Output: &output},
	}); err != nil {
		t.Fatal(err)
	}
	if got := count(t, database, `SELECT COUNT(*) FROM index_staging_files WHERE run_id='run' AND status='walked'`); got != 1 {
		t.Fatalf("walked rows = %d, want 1", got)
	}
	if got := count(t, database, `SELECT COUNT(*) FROM index_staging_files WHERE run_id='run' AND rel_path='indexed.go' AND source_hash='hash' AND status='indexed'`); got != 1 {
		t.Fatalf("indexed rows = %d, want 1", got)
	}
	for _, table := range []string{"index_staging_file_nodes", "index_staging_file_edges", "index_staging_file_iir"} {
		if got := count(t, database, `SELECT COUNT(*) FROM `+table+` WHERE run_id='run' AND rel_path='indexed.go'`); got != 1 {
			t.Fatalf("%s rows = %d, want 1", table, got)
		}
	}
	if err := q.StageFileEvents(ctx, run, project, []queries.StagedFileEvent{{Path: "would-rollback.go"}, {}}); err == nil {
		t.Fatal("expected empty path to reject whole batch")
	}
	if got := count(t, database, `SELECT COUNT(*) FROM index_staging_files WHERE run_id='run' AND rel_path='would-rollback.go'`); got != 0 {
		t.Fatalf("invalid batch committed %d rows", got)
	}
}

func TestLatestCompletedExtractorFingerprint(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)
	const project = "p"
	for _, entry := range []struct {
		id, fingerprint string
		completedAt     int
	}{
		{"old", "old-fingerprint", 2},
		{"new", "new-fingerprint", 3},
	} {
		if err := q.StartIndexRun(ctx, entry.id, project, nil, entry.fingerprint, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE index_runs SET status='completed', completed_at=? WHERE id=?`, entry.completedAt, entry.id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := q.LatestCompletedExtractorFingerprint(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	if got != "new-fingerprint" {
		t.Fatalf("fingerprint = %q, want new-fingerprint", got)
	}
	missing, err := q.LatestCompletedExtractorFingerprint(ctx, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if missing != "" {
		t.Fatalf("missing fingerprint = %q, want empty", missing)
	}
}

func TestReconcileStagedIndexRunKeepsUnchangedWalkedFileOwnership(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)
	const project, oldRun, run = "p", "old", "new"
	if _, err := database.Exec(`INSERT INTO nodes (id,project_id,type,label,canonical_id,source_class,index_managed,last_index_run_id,created_at,updated_at,properties) VALUES ('stable',?,'symbol','stable','stable','structural',1,?,1,1,'{}')`, project, oldRun); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO index_file_nodes (project_id,rel_path,node_id,last_seen_run_id) VALUES ('p','unchanged.go','stable','old')`,
		`INSERT INTO file_hashes (project_id,rel_path,hash,indexed_at) VALUES ('p','unchanged.go','hash',1)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.StartIndexRun(ctx, run, project, nil, "fingerprint", 2); err != nil {
		t.Fatal(err)
	}
	if err := q.StageWalked(ctx, run, project, "unchanged.go"); err != nil {
		t.Fatal(err)
	}
	if err := q.ReconcileStagedIndexRun(ctx, project, run, false, 0, 0, 0, 3); err != nil {
		t.Fatal(err)
	}
	if !nodeExists(t, database, "stable") {
		t.Fatal("unchanged file's node was removed")
	}
	if got := count(t, database, `SELECT COUNT(*) FROM index_file_nodes WHERE project_id='p' AND rel_path='unchanged.go' AND node_id='stable'`); got != 1 {
		t.Fatalf("unchanged ownership rows = %d, want 1", got)
	}
	if got := count(t, database, `SELECT COUNT(*) FROM file_hashes WHERE project_id='p' AND rel_path='unchanged.go' AND hash='hash'`); got != 1 {
		t.Fatalf("unchanged hash rows = %d, want 1", got)
	}
}

func TestReconcileStagedIndexRunPathsOnlyReplacesRequestedPaths(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)
	const project, oldRun, run = "p", "old", "targeted"
	for _, id := range []string{"keep", "gone"} {
		if _, err := database.Exec(`INSERT INTO nodes (id,project_id,type,label,canonical_id,source_class,index_managed,last_index_run_id,created_at,updated_at,properties) VALUES (?,?,'symbol',?,?, 'structural',1,?,1,1,'{}')`, id, project, id, id, oldRun); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO index_file_nodes (project_id,rel_path,node_id,last_seen_run_id) VALUES ('p','keep.go','keep','old')`,
		`INSERT INTO index_file_nodes (project_id,rel_path,node_id,last_seen_run_id) VALUES ('p','gone.go','gone','old')`,
		`INSERT INTO file_hashes (project_id,rel_path,hash,indexed_at) VALUES ('p','keep.go','keep-hash',1)`,
		`INSERT INTO file_hashes (project_id,rel_path,hash,indexed_at) VALUES ('p','gone.go','gone-hash',1)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.StartIndexRun(ctx, run, project, nil, "fingerprint", 2); err != nil {
		t.Fatal(err)
	}
	if err := q.StageWalked(ctx, run, project, "keep.go"); err != nil {
		t.Fatal(err)
	}
	if err := q.StageDeleted(ctx, run, project, "gone.go"); err != nil {
		t.Fatal(err)
	}
	if err := q.ReconcileStagedIndexRunPaths(ctx, project, run, 0, 0, 0, 3); err != nil {
		t.Fatal(err)
	}
	if !nodeExists(t, database, "keep") {
		t.Fatal("unmodified requested path lost its contribution")
	}
	if nodeExists(t, database, "gone") {
		t.Fatal("deleted requested path retained its contribution")
	}
	if got := count(t, database, `SELECT COUNT(*) FROM file_hashes WHERE project_id='p' AND rel_path='keep.go'`); got != 1 {
		t.Fatalf("kept hash rows = %d, want 1", got)
	}
	if got := count(t, database, `SELECT COUNT(*) FROM file_hashes WHERE project_id='p' AND rel_path='gone.go'`); got != 0 {
		t.Fatalf("deleted hash rows = %d, want 0", got)
	}
}
