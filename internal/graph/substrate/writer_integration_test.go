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
