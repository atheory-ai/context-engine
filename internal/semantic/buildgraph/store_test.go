package buildgraph

import (
	"context"
	"errors"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

func TestReadScopedStoreCannotCreateSemanticRecords(t *testing.T) {
	writer := &captureWriter{}
	store := NewStore(writer, false)
	if err := store.PersistPlan(context.Background(), fixturePlan(t), Context{}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("PersistPlan error = %v, want ErrReadOnly", err)
	}
	if writer.planWrites != 0 {
		t.Fatalf("read-scoped store wrote %d plan records", writer.planWrites)
	}
}

func TestWritableStorePersistsCanonicalPlan(t *testing.T) {
	writer := &captureWriter{}
	store := NewStore(writer, true)
	store.now = func() int64 { return 42 }
	p := fixturePlan(t)
	if err := store.PersistPlan(context.Background(), p, Context{RunID: "run", TurnID: "turn"}); err != nil {
		t.Fatal(err)
	}
	if writer.planWrites != 1 || writer.plan.ID != p.ID || writer.plan.CreatedAt != 42 || writer.plan.Payload == "" {
		t.Fatalf("record = %+v writes=%d", writer.plan, writer.planWrites)
	}
}

func fixturePlan(t *testing.T) *plan.SemanticPlan {
	t.Helper()
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: update\nlanguage: typescript\norigin: declared\nreturns:\n  type: void\nsideEffects: []\nfailureModes: []\nconstraints: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("project", plan.SemanticUnit{ID: "unit", CanonicalID: "fixture.update", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type captureWriter struct {
	planWrites int
	plan       core.SemanticPlanRecord
}

func (w *captureWriter) UpsertSemanticPlan(_ context.Context, r core.SemanticPlanRecord) error {
	w.planWrites++
	w.plan = r
	return nil
}
func (w *captureWriter) UpsertSemanticRecipe(context.Context, core.SemanticRecipeRecord) error {
	return nil
}
func (w *captureWriter) UpsertSemanticArtifact(context.Context, core.SemanticArtifactRecord) error {
	return nil
}
func (w *captureWriter) RecordSemanticVerification(context.Context, core.SemanticVerificationRecord) error {
	return nil
}
func (w *captureWriter) RecordSemanticApproval(context.Context, core.SemanticApprovalRecord) error {
	return nil
}
func (w *captureWriter) UpsertSemanticTestPlan(context.Context, core.SemanticTestPlanRecord) error {
	return nil
}
func (w *captureWriter) UpsertSemanticRepair(context.Context, core.SemanticRepairRecord) error {
	return nil
}
