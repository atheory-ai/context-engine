package runner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
)

func TestSearchSubstrateTokenizedRankingAndSourceMetadata(t *testing.T) {
	ctx := context.Background()
	reg := db.NewRegistry()
	graphPath := filepath.Join(t.TempDir(), "local.db")
	if err := reg.Mount("local", graphPath); err != nil {
		t.Fatalf("mount graph: %v", err)
	}
	graphDB, err := reg.GraphDB("local")
	if err != nil {
		t.Fatalf("graph db: %v", err)
	}
	if err := migrations.RunGraph(graphDB); err != nil {
		t.Fatalf("migrate graph: %v", err)
	}

	_, err = graphDB.ExecContext(ctx, `
		INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, created_at, updated_at, properties)
		VALUES
		('file-1', 'local', 'file', 'CartController.php', 'woocommerce/src/StoreApi/Utilities/CartController.php', 'structural', 1, 1, '{}'),
		('sym-1', 'local', 'symbol', 'CartController.load_cart', 'Automattic\\WooCommerce\\StoreApi\\Utilities\\CartController.load_cart', 'structural', 1, 1, '{"file_path":"woocommerce/src/StoreApi/Utilities/CartController.php","line_start":44,"line_end":72}'),
		('sym-2', 'local', 'symbol', 'CartTokenUtils.get_cart_token', 'Automattic\\WooCommerce\\StoreApi\\Utilities\\CartTokenUtils.get_cart_token', 'structural', 1, 1, '{"file_path":"woocommerce/src/StoreApi/Utilities/CartTokenUtils.php","start_line":23,"end_line":31}'),
		('sym-3', 'local', 'symbol', 'Unrelated', 'Example\\Unrelated', 'structural', 1, 1, '{"file_path":"src/Other.php"}')
	`)
	if err != nil {
		t.Fatalf("insert nodes: %v", err)
	}

	engine := &Engine{dbRegistry: reg}
	results, err := engine.SearchSubstrate(ctx, SearchOptions{Query: "CartController load_cart", Limit: 5})
	if err != nil {
		t.Fatalf("SearchSubstrate: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	if results[0].ID != "sym-1" {
		t.Fatalf("top result id = %s, want sym-1; results=%#v", results[0].ID, results)
	}
	if results[0].FilePath != "woocommerce/src/StoreApi/Utilities/CartController.php" {
		t.Fatalf("file path = %q", results[0].FilePath)
	}
	if results[0].LineStart != 44 || results[0].LineEnd != 72 {
		t.Fatalf("line range = %d-%d", results[0].LineStart, results[0].LineEnd)
	}
	if results[0].MatchReason == "" {
		t.Fatal("expected match reason")
	}

	results, err = engine.SearchSubstrate(ctx, SearchOptions{Query: "Store API cart token", Limit: 5})
	if err != nil {
		t.Fatalf("SearchSubstrate store api: %v", err)
	}
	foundToken := false
	for _, result := range results {
		if result.ID == "sym-2" {
			foundToken = true
			if result.LineStart != 23 || result.LineEnd != 31 {
				t.Fatalf("start_line/end_line range = %d-%d", result.LineStart, result.LineEnd)
			}
		}
	}
	if !foundToken {
		t.Fatalf("expected token utility result for semantic multi-term query; results=%#v", results)
	}
}
