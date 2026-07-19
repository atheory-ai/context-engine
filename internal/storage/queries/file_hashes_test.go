package queries_test

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

func TestReconcileFileHashesPreservesIncrementalFailuresAndReplacesFullSet(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	q := queries.NewIndexQueries(database)

	if err := q.UpsertFileHash(ctx, "project", "unchanged.go", "old-unchanged"); err != nil {
		t.Fatal(err)
	}
	if err := q.UpsertFileHash(ctx, "project", "changed.go", "old-changed"); err != nil {
		t.Fatal(err)
	}

	// An incremental run only advances files whose graph writes completed.
	if err := q.ReconcileFileHashes(ctx, "project", map[string]string{"changed.go": "new-changed"}, false); err != nil {
		t.Fatal(err)
	}
	hashes, err := q.GetFileHashes(ctx, "project")
	if err != nil {
		t.Fatal(err)
	}
	if hashes["unchanged.go"] != "old-unchanged" || hashes["changed.go"] != "new-changed" {
		t.Fatalf("incremental hashes = %#v", hashes)
	}

	// A successful full run atomically replaces the complete set.
	if err := q.ReconcileFileHashes(ctx, "project", map[string]string{"fresh.go": "fresh"}, true); err != nil {
		t.Fatal(err)
	}
	hashes, err = q.GetFileHashes(ctx, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 1 || hashes["fresh.go"] != "fresh" {
		t.Fatalf("full hashes = %#v", hashes)
	}
}
