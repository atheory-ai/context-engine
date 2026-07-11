package orggraph_test

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/orggraph"
	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
)

func TestOrgConceptSeeds_RoundTrip(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := migrations.RunGraph(d); err != nil {
		t.Fatalf("migrate graph: %v", err)
	}
	if err := migrations.RunOrg(d); err != nil {
		t.Fatalf("migrate org: %v", err)
	}

	g := orggraph.OpenFromDB(d)
	ctx := context.Background()

	seed := core.ConceptSeed{
		Term:       "payment",
		Definition: "money movement",
		Related:    []string{"billing", "invoice"},
		Synonyms:   []string{"pay", "charge"},
	}
	if err := g.AddOrgConceptSeed(ctx, seed); err != nil {
		t.Fatalf("AddOrgConceptSeed: %v", err)
	}

	got, err := g.GetOrgConceptSeeds(ctx)
	if err != nil {
		t.Fatalf("GetOrgConceptSeeds: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d seeds, want 1", len(got))
	}
	if got[0].Term != "payment" || got[0].Definition != "money movement" {
		t.Errorf("seed = %+v", got[0])
	}
	// Related/Synonyms round-trip through JSON columns.
	if len(got[0].Related) != 2 || got[0].Related[0] != "billing" {
		t.Errorf("related = %v", got[0].Related)
	}
	if len(got[0].Synonyms) != 2 {
		t.Errorf("synonyms = %v", got[0].Synonyms)
	}

	// Upsert-on-conflict: adding the same term updates rather than duplicates.
	seed.Definition = "updated"
	if err := g.AddOrgConceptSeed(ctx, seed); err != nil {
		t.Fatalf("AddOrgConceptSeed (update): %v", err)
	}
	got, _ = g.GetOrgConceptSeeds(ctx)
	if len(got) != 1 || got[0].Definition != "updated" {
		t.Errorf("upsert did not update in place: %+v", got)
	}

	if err := g.RemoveOrgConceptSeed(ctx, "payment"); err != nil {
		t.Fatalf("RemoveOrgConceptSeed: %v", err)
	}
	got, _ = g.GetOrgConceptSeeds(ctx)
	if len(got) != 0 {
		t.Errorf("expected no seeds after remove, got %d", len(got))
	}
}
