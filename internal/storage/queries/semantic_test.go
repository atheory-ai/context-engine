package queries_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/atheory-ai/context-engine/internal/storage/queries"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

func TestSemanticHistoryAndDiff(t *testing.T) {
	database := migratedGraph(t)
	ctx := context.Background()
	buf := writebuffer.New(ctx, semanticProvider{db: database}, 1024, 5*time.Millisecond)
	defer buf.Close(ctx)
	for _, op := range []writebuffer.WriteOp{
		{Type: writebuffer.OpUpsertSemanticPlan, ProjectID: "p", Payload: writebuffer.SemanticPlanUpsert{ID: "p1", ProjectID: "p", UnitID: "unit", Revision: 1, Lifecycle: "resolved", SchemaVersion: "v1", Payload: `{"bindings":[{"id":"binding","value":"old"}],"claims":[{"id":"claim","value":"old"}],"obligations":[],"decisions":[],"openQuestions":[{"id":"question"}]}`, CreatedAt: 1}},
		{Type: writebuffer.OpUpsertSemanticPlan, ProjectID: "p", Payload: writebuffer.SemanticPlanUpsert{ID: "p2", ProjectID: "p", UnitID: "unit", ParentPlanID: "p1", Revision: 2, Lifecycle: "resolved", SchemaVersion: "v1", Payload: `{"bindings":[{"id":"binding","value":"new"}],"claims":[{"id":"claim","value":"new"}],"obligations":[{"id":"audit"}],"decisions":[],"openQuestions":[]}`, CreatedAt: 2}},
		{Type: writebuffer.OpUpsertSemanticRecipe, ProjectID: "p", Payload: writebuffer.SemanticRecipeUpsert{ID: "r1", ProjectID: "p", PlanRevisionID: "p1", SchemaVersion: "v1", TargetLanguage: "typescript", RendererProfile: `{}`, Payload: `{"steps":[{"id":"step","operation":"old"}]}`, CreatedAt: 1}},
		{Type: writebuffer.OpUpsertSemanticRecipe, ProjectID: "p", Payload: writebuffer.SemanticRecipeUpsert{ID: "r2", ProjectID: "p", PlanRevisionID: "p2", SchemaVersion: "v1", TargetLanguage: "typescript", RendererProfile: `{}`, Payload: `{"steps":[{"id":"step","operation":"new"}]}`, CreatedAt: 2}},
	} {
		if err := buf.Send(op); err != nil {
			t.Fatal(err)
		}
	}
	if err := buf.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := queries.ListSemanticPlanHistory(ctx, database, "p", "unit")
	if err != nil || len(history) != 2 || history[1].ID != "p2" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	diff, err := queries.DiffSemanticPlans(ctx, database, "p1", "p2")
	if err != nil || len(diff.Bindings) != 1 || len(diff.Claims) != 1 || len(diff.Obligations) != 1 || len(diff.RecipeSteps) != 1 {
		t.Fatalf("diff=%+v err=%v", diff, err)
	}
	questions, err := queries.UnresolvedSemanticQuestions(ctx, database, "p1")
	if err != nil || len(questions) != 1 {
		t.Fatalf("questions=%s err=%v", questions, err)
	}
}

type semanticProvider struct{ db *sql.DB }

func (p semanticProvider) GraphDB(string) (*sql.DB, error) { return p.db, nil }
