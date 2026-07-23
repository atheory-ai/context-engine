package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/packet"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

func TestPrepareImplementationContract_DeclaredIntentNeedsNoModel(t *testing.T) {
	dir := t.TempDir()
	intentPath := filepath.Join(dir, "intent.yaml")
	if err := os.WriteFile(intentPath, []byte("kind: FunctionIntent\nname: validate\nlanguage: typescript\nreturns:\n  type: Result\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareImplementationContract(context.Background(), &config.Config{DataDir: dir}, intentPath, "", "Validation.validate", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Executor != "declared" || prepared.Packet.Status != packet.StatusReady {
		t.Fatalf("prepared = %#v", prepared)
	}
	if prepared.Plan.Unit.CanonicalID != "Validation.validate" || prepared.Plan.Intent.Name != "validate" {
		t.Fatalf("plan = %#v", prepared.Plan)
	}
}

func TestPrepareImplementationContract_BlocksWithoutTarget(t *testing.T) {
	dir := t.TempDir()
	intentPath := filepath.Join(dir, "intent.yaml")
	if err := os.WriteFile(intentPath, []byte("kind: FunctionIntent\nname: validate\nlanguage: typescript\nreturns:\n  type: Result\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareImplementationContract(context.Background(), &config.Config{DataDir: dir}, intentPath, "", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Packet.Status != packet.StatusBlocked {
		t.Fatalf("packet status = %q, want blocked", prepared.Packet.Status)
	}
}

func TestAddDeclaredContextProducesProvenancedClaim(t *testing.T) {
	dir := t.TempDir()
	intentPath := filepath.Join(dir, "intent.yaml")
	if err := os.WriteFile(intentPath, []byte("kind: FunctionIntent\nname: validate\nlanguage: php\nreturns:\n  type: WP_Error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	semanticPlan, _, err := prepareInitialPlan(context.Background(), &config.Config{DataDir: dir}, intentPath, "", "Checkout.validate", "")
	if err != nil {
		t.Fatal(err)
	}
	decorated, err := addDeclaredContext(semanticPlan, []string{"woocommerce.checkout", "woocommerce.checkout"})
	if err != nil {
		t.Fatal(err)
	}
	if decorated.Revision != 2 || len(decorated.Claims) != 1 || decorated.Claims[0].Kind != "context.woocommerce.checkout" {
		t.Fatalf("context plan = %#v", decorated)
	}
	if got := decorated.Claims[0].Evidence[0].Source; got != "user" {
		t.Errorf("context evidence source = %q", got)
	}
}

func TestAddDeclaredTagsAcceptsControlledVocabularyAndRejectsUnknowns(t *testing.T) {
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: updateCart\nlanguage: php\nreturns:\n  type: bool\n"))
	if err != nil {
		t.Fatal(err)
	}
	semanticPlan, err := plan.NewPlan("p", plan.SemanticUnit{ID: "u", CanonicalID: "Cart.update", Scope: "function", Language: "php", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	decorated, err := addDeclaredTags(semanticPlan, []string{"woocommerce.cart"}, []string{"operation.cart.modify", "input.user_controlled"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, claim := range decorated.Claims {
		kinds[claim.Kind] = true
	}
	for _, want := range []string{"context.woocommerce.cart", "operation.cart.modify", "input.user_controlled"} {
		if !kinds[want] {
			t.Fatalf("missing declared tag %q: %#v", want, decorated.Claims)
		}
	}
	if _, err := addDeclaredTags(semanticPlan, nil, []string{"operation.typo"}); err == nil {
		t.Fatal("expected unknown controlled tag error")
	}
}
