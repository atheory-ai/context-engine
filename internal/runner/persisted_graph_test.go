package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
)

func TestNewMountsPersistedLocalGraph(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	graphPath := filepath.Join(dataDir, "graphs", "local.db")
	if err := os.MkdirAll(filepath.Dir(graphPath), 0o755); err != nil {
		t.Fatalf("create graph directory: %v", err)
	}

	seed := db.NewRegistry()
	if err := seed.Mount("local", graphPath); err != nil {
		t.Fatalf("mount seed graph: %v", err)
	}
	graphDB, err := seed.GraphDB("local")
	if err != nil {
		t.Fatalf("get seed graph: %v", err)
	}
	if err := migrations.RunGraph(graphDB); err != nil {
		t.Fatalf("migrate seed graph: %v", err)
	}
	if _, err := graphDB.ExecContext(ctx, `
		INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, created_at, updated_at, properties)
		VALUES ('fixture-file', 'local', 'file', 'wordpress-hooks.php', 'demo/fixtures/php-iir/wordpress-hooks.php', 'structural', 1, 1, '{}')
	`); err != nil {
		t.Fatalf("seed graph node: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed graph: %v", err)
	}

	engine, err := New(ctx, &config.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer engine.Close(context.Background())

	nodes, err := engine.SearchSubstrate(ctx, SearchOptions{Query: "wordpress-hooks", Type: "file", Limit: 1})
	if err != nil {
		t.Fatalf("search persisted graph: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "fixture-file" {
		t.Fatalf("search results = %#v, want persisted fixture file", nodes)
	}
}
