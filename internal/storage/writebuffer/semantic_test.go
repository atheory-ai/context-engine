package writebuffer_test

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

func TestSemanticBuildWritesAreOrderedAndIdempotent(t *testing.T) {
	graphDB := setupGraphDB(t)
	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)
	now := int64(1)
	ops := []writebuffer.WriteOp{
		{Type: writebuffer.OpUpsertSemanticRepair, ProjectID: "project", Payload: writebuffer.SemanticRepairUpsert{ID: "repair", ProjectID: "project", PlanRevisionID: "plan", RecipeID: "recipe", VerificationID: "verification", Status: "proposed", Payload: `{}`, CreatedAt: now}},
		{Type: writebuffer.OpRecordSemanticVerification, ProjectID: "project", Payload: writebuffer.SemanticVerificationRecord{ID: "verification", ProjectID: "project", PlanRevisionID: "plan", RecipeID: "recipe", ArtifactID: "artifact", Verdict: "passed", VerifierVersion: "v1", Payload: `{}`, CreatedAt: now}},
		{Type: writebuffer.OpUpsertSemanticArtifact, ProjectID: "project", Payload: writebuffer.SemanticArtifactUpsert{ID: "artifact", ProjectID: "project", PlanRevisionID: "plan", RecipeID: "recipe", Kind: "source", ContentHash: "hash", TargetLanguage: "typescript", TargetPath: "src/update.ts", CreatedAt: now}},
		{Type: writebuffer.OpUpsertSemanticRecipe, ProjectID: "project", Payload: writebuffer.SemanticRecipeUpsert{ID: "recipe", ProjectID: "project", PlanRevisionID: "plan", SchemaVersion: "v1", TargetLanguage: "typescript", RendererProfile: `{}`, Payload: `{}`, CreatedAt: now}},
		{Type: writebuffer.OpUpsertSemanticPlan, ProjectID: "project", Payload: writebuffer.SemanticPlanUpsert{ID: "plan", ProjectID: "project", UnitID: "unit", Revision: 1, Lifecycle: "resolved", SchemaVersion: "v1", Payload: `{}`, CreatedAt: now}},
	}
	for _, op := range ops {
		if err := buf.Send(op); err != nil {
			t.Fatal(err)
		}
	}
	if err := buf.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"semantic_plans", "semantic_recipes", "semantic_artifacts", "semantic_verifications", "semantic_repairs"} {
		var count int
		if err := graphDB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	// Replaying immutable IDs must retain exactly one row per record.
	if err := buf.Send(ops[4]); err != nil {
		t.Fatal(err)
	}
	if err := buf.Send(ops[3]); err != nil {
		t.Fatal(err)
	}
	if err := buf.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	var plans, recipes int
	if err := graphDB.QueryRow(`SELECT COUNT(*) FROM semantic_plans`).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if err := graphDB.QueryRow(`SELECT COUNT(*) FROM semantic_recipes`).Scan(&recipes); err != nil {
		t.Fatal(err)
	}
	if plans != 1 || recipes != 1 {
		t.Fatalf("replay created rows: plans=%d recipes=%d", plans, recipes)
	}
}

func TestSemanticArtifactRejectsUnpermittedContent(t *testing.T) {
	graphDB := setupGraphDB(t)
	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)
	for _, op := range []writebuffer.WriteOp{
		{Type: writebuffer.OpUpsertSemanticPlan, ProjectID: "p", Payload: writebuffer.SemanticPlanUpsert{ID: "p1", ProjectID: "p", UnitID: "u", Revision: 1, Lifecycle: "resolved", SchemaVersion: "v1", Payload: `{}`, CreatedAt: 1}},
		{Type: writebuffer.OpUpsertSemanticRecipe, ProjectID: "p", Payload: writebuffer.SemanticRecipeUpsert{ID: "r1", ProjectID: "p", PlanRevisionID: "p1", SchemaVersion: "v1", TargetLanguage: "typescript", RendererProfile: `{}`, Payload: `{}`, CreatedAt: 1}},
		{Type: writebuffer.OpUpsertSemanticArtifact, ProjectID: "p", Payload: writebuffer.SemanticArtifactUpsert{ID: "a1", ProjectID: "p", PlanRevisionID: "p1", RecipeID: "r1", Kind: "source", ContentHash: "h", TargetLanguage: "typescript", TargetPath: "a.ts", SourceContent: "secret", CreatedAt: 1}},
	} {
		if err := buf.Send(op); err != nil {
			t.Fatal(err)
		}
	}
	// The fire-and-forget buffer logs transaction failures rather than returning
	// them to the caller; the observable contract is that invalid content is
	// absent after flush.
	_ = buf.Flush(ctx)
	var count int
	if err := graphDB.QueryRow(`SELECT COUNT(*) FROM semantic_artifacts WHERE id = 'a1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("retention constraint accepted source content without permission")
	}
}
