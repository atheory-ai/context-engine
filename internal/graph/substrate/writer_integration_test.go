package substrate_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/graph/substrate"
	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

type testProvider struct {
	db *sql.DB
}

func (p *testProvider) GraphDB(_ string) (*sql.DB, error) {
	return p.db, nil
}

func TestSubstrateWriterQueuesWritesUntilFlush(t *testing.T) {
	ctx := context.Background()
	graphDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	t.Cleanup(func() { graphDB.Close() })
	if err := migrations.RunGraph(graphDB); err != nil {
		t.Fatalf("migrate graph db: %v", err)
	}

	provider := &testProvider{db: graphDB}
	buf := writebuffer.New(ctx, provider, 1024, time.Hour)
	t.Cleanup(func() { _ = buf.Close(context.Background()) })
	writer := substrate.NewWriter(buf, provider)

	node := core.Node{
		ID:          "node-queued",
		ProjectID:   "project-queued",
		Type:        core.NodeTypeSymbol,
		Label:       "Queued",
		CanonicalID: "pkg.Queued",
		SourceClass: core.SourceStructural,
		Properties:  map[string]any{"file": "queued.go"},
		CreatedAt:   1,
		UpdatedAt:   1,
	}
	if err := writer.UpsertNode(ctx, node); err != nil {
		t.Fatalf("upsert node: %v", err)
	}

	assertNodeCount(t, graphDB, 0)

	if err := writer.Flush(ctx); err != nil {
		t.Fatalf("flush writer: %v", err)
	}
	assertNodeCount(t, graphDB, 1)

	if err := writer.UpdateActivationForProject(ctx, node.ProjectID, node.ID, 0.75); err != nil {
		t.Fatalf("update activation: %v", err)
	}
	if err := writer.Flush(ctx); err != nil {
		t.Fatalf("flush activation: %v", err)
	}

	var activation float64
	if err := graphDB.QueryRow(`SELECT activation FROM node_activation WHERE node_id = ?`, node.ID).Scan(&activation); err != nil {
		t.Fatalf("query activation: %v", err)
	}
	if activation != 0.75 {
		t.Fatalf("activation = %v, want 0.75", activation)
	}
}

// TestSourceFileRoundTripAndPrune drives the full incremental write path: nodes
// written through the real Writer carry their SourceFile into the DB, and the
// indexer's PruneFileNodes then removes a file's stale symbols while keeping the
// ones re-emitted this run.
func TestSourceFileRoundTripAndPrune(t *testing.T) {
	ctx := context.Background()
	graphDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	t.Cleanup(func() { graphDB.Close() })
	if err := migrations.RunGraph(graphDB); err != nil {
		t.Fatalf("migrate graph db: %v", err)
	}
	provider := &testProvider{db: graphDB}
	buf := writebuffer.New(ctx, provider, 1024, time.Hour)
	t.Cleanup(func() { _ = buf.Close(context.Background()) })
	writer := substrate.NewWriter(buf, provider)

	const proj = core.ProjectID("p")
	mk := func(id, typ, file string) core.Node {
		return core.Node{ID: core.NodeID(id), ProjectID: proj, Type: typ, Label: id,
			CanonicalID: id, SourceClass: core.SourceStructural, SourceFile: file, CreatedAt: 1, UpdatedAt: 1}
	}
	for _, n := range []core.Node{
		mk("survivor", core.NodeTypeSymbol, "a.go"),
		mk("removed", core.NodeTypeSymbol, "a.go"),
		mk("other", core.NodeTypeSymbol, "b.go"),
	} {
		if err := writer.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert %s: %v", n.ID, err)
		}
	}
	if err := writer.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// SourceFile survived the Writer → buffer → DB round trip.
	var gotFile string
	if err := graphDB.QueryRow(`SELECT source_file FROM nodes WHERE id = 'survivor'`).Scan(&gotFile); err != nil {
		t.Fatalf("read source_file: %v", err)
	}
	if gotFile != "a.go" {
		t.Fatalf("source_file = %q, want a.go", gotFile)
	}

	// Prune a.go keeping only 'survivor' — 'removed' goes, others stay.
	if _, err := queries.NewIndexQueries(graphDB).PruneFileNodes(ctx, string(proj), "a.go", []string{"survivor"}); err != nil {
		t.Fatalf("PruneFileNodes: %v", err)
	}
	for id, want := range map[string]bool{"survivor": true, "removed": false, "other": true} {
		got := false
		if err := graphDB.QueryRow(`SELECT COUNT(*) > 0 FROM nodes WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("check %s: %v", id, err)
		}
		if got != want {
			t.Errorf("node %s present=%v, want %v", id, got, want)
		}
	}
}

func assertNodeCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&count); err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if count != want {
		t.Fatalf("node count = %d, want %d", count, want)
	}
}
