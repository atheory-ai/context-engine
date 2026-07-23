package packet

import (
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

func TestBuildKeepsPluginRequirementAndBlocksOnQuestions(t *testing.T) {
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: validate\nlanguage: php\nreturns:\n  type: WP_Error\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("project", plan.SemanticUnit{ID: "unit", CanonicalID: "Checkout.validate", Scope: "function", Language: "php", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	p.Obligations = []plan.Obligation{{ID: "hook", Kind: "hook", Requirement: "invoke woocommerce_checkout_process", State: plan.KnowledgeDeclared, Evidence: []plan.Evidence{{ID: "hook-evidence", Source: "policy", Producer: "plugin:wordpress@1.0.0", Explanation: "fixture"}}}}
	p.OpenQuestions = []plan.OpenQuestion{{ID: "target", Prompt: "Which checkout extension point?", Blocking: true, State: plan.KnowledgeUnknown, Evidence: []plan.Evidence{{ID: "target-evidence", Source: "unknown", Producer: "test", Explanation: "fixture"}}, Candidates: []plan.Candidate{}}}

	packet, err := Build(p)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Status != StatusBlocked || len(packet.Requirements) != 1 || packet.Requirements[0].Producer != "plugin:wordpress@1.0.0" {
		t.Fatalf("packet = %#v", packet)
	}
	prompt, err := Prompt(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "woocommerce_checkout_process") || !strings.Contains(prompt, "Do not write source") {
		t.Fatalf("prompt = %q", prompt)
	}
}
