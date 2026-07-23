package decorate

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/passes"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

func fixturePlan(t *testing.T, language string) *plan.SemanticPlan {
	t.Helper()
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: validate\nlanguage: " + language + "\nreturns:\n  type: Result\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("project", plan.SemanticUnit{ID: "unit", CanonicalID: "requested.validate", Scope: "function", Language: language, SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	p.Claims = []plan.SemanticClaim{{ID: "checkout", Kind: "context.checkout", Statement: "checkout validation", State: plan.KnowledgeObserved, Evidence: []plan.Evidence{{ID: "checkout-evidence", Source: "structural", Producer: "test", Explanation: "fixture"}}}}
	return p
}

func TestApplyDecoratesApplicablePluginRequirementsWithOrigin(t *testing.T) {
	source := fixturePlan(t, "php")
	result, err := Apply(source, Input{Plugins: []Contribution{{
		PluginID: "com.example.wordpress", Version: "1.2.3", Raw: []byte(`{
			"schemaVersion":"v1", "languages":["php"], "policies":[{
				"id":"wordpress.checkout.hook", "version":"v1", "phase":"constrain", "priority":10,
				"when":{"claimKinds":["context.checkout"]}, "severity":"error",
				"add":{"kind":"hook","requirement":"invoke woocommerce_checkout_process","mandatory":true}
			}]}`),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AppliedPluginIDs) != 1 || result.AppliedPluginIDs[0] != "com.example.wordpress" {
		t.Fatalf("applied plugins = %#v", result.AppliedPluginIDs)
	}
	if len(result.Plan.Obligations) != 1 {
		t.Fatalf("obligations = %#v", result.Plan.Obligations)
	}
	if got := result.Plan.Obligations[0].Evidence[0].Producer; got != "plugin:com.example.wordpress@1.2.3" {
		t.Errorf("obligation producer = %q", got)
	}
}

func TestApplySkipsNonMatchingPluginLanguage(t *testing.T) {
	result, err := Apply(fixturePlan(t, "typescript"), Input{Plugins: []Contribution{{
		PluginID: "com.example.wordpress", Raw: []byte(`{"schemaVersion":"v1","languages":["php"],"policies":[{"id":"wordpress.hook","version":"v1","phase":"constrain","severity":"error"}]}`),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Plan.Obligations) != 0 || len(result.SkippedPluginIDs) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestApplyRejectsPolicyConflictInsteadOfChoosingOne(t *testing.T) {
	source := fixturePlan(t, "php")
	base := passes.Policy{ID: "base.hook", Version: "v1", Phase: passes.PhaseConstrain, Severity: passes.SeverityError, Add: &passes.Obligation{Kind: "hook", Requirement: "first", Mandatory: true}}
	// Conflicts are a valid decoration result: passes records the blocking
	// question rather than silently preferring the later contribution.
	result, err := Apply(source, Input{BuiltIn: []passes.Policy{base}, Plugins: []Contribution{{
		PluginID: "com.example.wordpress", Raw: []byte(`{"schemaVersion":"v1","policies":[{"id":"wordpress.hook","version":"v1","phase":"constrain","severity":"error","add":{"kind":"hook","requirement":"second","mandatory":true}}]}`),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Lifecycle != plan.LifecycleBlocked || len(result.Findings) != 1 {
		t.Fatalf("conflict result = %#v", result)
	}
}
