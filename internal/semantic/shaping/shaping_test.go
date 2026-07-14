package shaping

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

type fakeIntentShaper struct {
	intent *iir.FunctionIntent
	err    error
	calls  int
}

func (f *fakeIntentShaper) Shape(context.Context, string) (*iir.FunctionIntent, error) {
	f.calls++
	return f.intent, f.err
}

func declaredIntent(t *testing.T) *iir.FunctionIntent {
	t.Helper()
	intent, err := iir.LoadIntent([]byte(`
kind: FunctionIntent
name: updateCustomer
language: typescript
inputs:
  - name: id
    type: string
returns:
  type: Customer
`))
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func TestFromIntent_CanonicalizesDeclaredInputWithoutModel(t *testing.T) {
	planner := NewWithShaper(nil)
	semanticPlan, err := planner.FromIntent(Input{
		ProjectID: "project",
		Unit: plan.SemanticUnit{
			ID:          "update-customer",
			NodeID:      "customer-update",
			CanonicalID: "CustomerService.updateCustomer",
			Scope:       "function",
			Language:    "typescript",
			SourceRefs:  []plan.SourceRef{},
		},
		Intent: declaredIntent(t),
	})
	if err != nil {
		t.Fatalf("FromIntent: %v", err)
	}
	if semanticPlan.Intent.Origin != iir.OriginDeclared || semanticPlan.Provenance[0].Source != "user" {
		t.Fatalf("declared provenance = intent:%q evidence:%+v", semanticPlan.Intent.Origin, semanticPlan.Provenance)
	}
	if len(semanticPlan.OpenQuestions) != 0 {
		t.Fatalf("unexpected open questions: %+v", semanticPlan.OpenQuestions)
	}
}

func TestShape_ProvenanceAndMissingRequirementsBecomeQuestions(t *testing.T) {
	intent := declaredIntent(t)
	intent.Origin = iir.OriginInferred
	shaper := &fakeIntentShaper{intent: intent}
	planner := NewWithShaper(shaper)
	semanticPlan, err := planner.Shape(context.Background(), Input{
		ProjectID:              core.ProjectID("project"),
		Description:            "Update the customer through the repository and return a provider-safe result.",
		RequiredBindings:       []Requirement{{ID: "repository", Prompt: "Which customer repository should be used?", Blocking: true}},
		RequireFailureStrategy: true,
	})
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	if shaper.calls != 1 {
		t.Fatalf("model calls = %d, want 1", shaper.calls)
	}
	if semanticPlan.Intent.Origin != iir.OriginInferred || semanticPlan.Provenance[0].Source != "model" {
		t.Fatalf("inferred provenance = intent:%q evidence:%+v", semanticPlan.Intent.Origin, semanticPlan.Provenance)
	}
	if len(semanticPlan.Provenance) < 9 {
		t.Fatalf("expected field-level model provenance, got %+v", semanticPlan.Provenance)
	}
	questions := map[string]plan.OpenQuestion{}
	for _, question := range semanticPlan.OpenQuestions {
		questions[question.ID] = question
	}
	for _, id := range []string{"target-symbol", "binding-repository", "failure-strategy"} {
		question, ok := questions[id]
		if !ok || !question.Blocking || question.State != plan.KnowledgeUnknown || len(question.Evidence) != 1 {
			t.Fatalf("question %q = %+v, found=%v", id, question, ok)
		}
	}
}

func TestShape_FailureStrategyQuestionNotAddedWhenIntentDefinesFailure(t *testing.T) {
	intent := declaredIntent(t)
	intent.Origin = iir.OriginInferred
	intent.FailureModes = []iir.FailureMode{{Code: "ProviderFailure"}}
	semanticPlan, err := NewWithShaper(&fakeIntentShaper{intent: intent}).Shape(context.Background(), Input{
		ProjectID:              "project",
		Description:            "Update the customer.",
		RequireFailureStrategy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, question := range semanticPlan.OpenQuestions {
		if question.ID == "failure-strategy" {
			t.Fatalf("unexpected failure strategy question: %+v", question)
		}
	}
}

func TestInputGuards(t *testing.T) {
	planner := NewWithShaper(nil)
	if _, err := planner.FromIntent(Input{}); err == nil {
		t.Fatal("expected declared intent error")
	}
	if _, err := planner.Shape(context.Background(), Input{Description: "x", Intent: declaredIntent(t)}); err == nil {
		t.Fatal("expected mutually exclusive input error")
	}
	if _, err := planner.Shape(context.Background(), Input{Description: "x"}); err == nil {
		t.Fatal("expected shaper configuration error")
	}
	if _, err := NewWithShaper(&fakeIntentShaper{}).Shape(context.Background(), Input{Description: "x"}); err == nil {
		t.Fatal("expected nil shaped intent error")
	}
}
