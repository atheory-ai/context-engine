package passes

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

func base(t *testing.T) *plan.SemanticPlan {
	t.Helper()
	intent, err := iir.LoadIntent([]byte("kind: FunctionIntent\nname: update\nlanguage: typescript\nreturns:\n  type: Customer\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.NewPlan("p", plan.SemanticUnit{ID: "u", CanonicalID: "requested.update", Scope: "function", Language: "typescript", SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		t.Fatal(err)
	}
	p.Claims = []plan.SemanticClaim{{ID: "mutation", Kind: "effect.mutation", Statement: "update", State: plan.KnowledgeObserved, Evidence: []plan.Evidence{{ID: "e", Source: "semantic", Producer: "test", Explanation: "fixture"}}}}
	return p
}

func policy(id string, phase Phase, priority int) Policy {
	return Policy{ID: id, Version: "v1", Phase: phase, Priority: priority, Severity: SeverityError}
}

func TestApplyOrdersAddsAndRequiresApproval(t *testing.T) {
	p := base(t)
	audit := policy("audit", PhaseConstrain, 2)
	audit.When = Selector{ClaimKinds: []string{"effect.mutation"}}
	audit.Add = &Obligation{Kind: "audit", Requirement: "emit event", Mandatory: true}
	audit.RequireApproval = true
	out, err := Apply(p, []Policy{audit})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Plan.Obligations) != 1 || len(out.Plan.OpenQuestions) != 1 || out.Plan.Revision != 2 {
		t.Fatalf("unexpected plan: %+v", out.Plan)
	}
	if out.Plan.Obligations[0].State != plan.KnowledgeDeclared {
		t.Fatalf("approval-gated obligation must be declared, got %q", out.Plan.Obligations[0].State)
	}
	if got := out.Plan.PassRecords[0].Outputs; len(got) != 1 || got[0] != "applied" {
		t.Fatalf("unexpected pass output: %v", got)
	}
}

func TestApplyUsesCompilerPhaseOrderAndRecordsSkips(t *testing.T) {
	p := base(t)
	pre := policy("pre", PhasePreGenerate, 0)
	resolve := policy("resolve", PhaseResolve, 10)
	constrain := policy("skipped", PhaseConstrain, 0)
	constrain.When = Selector{Languages: []string{"go"}}
	out, err := Apply(p, []Policy{pre, constrain, resolve})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := out.Plan.PassRecords[0].PassID, "resolve"; got != want {
		t.Fatalf("first policy = %q, want %q", got, want)
	}
	for _, record := range out.Plan.PassRecords {
		if record.PassID == "skipped" && (len(record.Outputs) != 1 || record.Outputs[0] != "skipped") {
			t.Fatalf("skipped policy history missing: %+v", record)
		}
	}
}

func TestApplyBlocksMandatoryConflict(t *testing.T) {
	p := base(t)
	a := policy("a", PhaseConstrain, 0)
	a.Add = &Obligation{Kind: "audit", Requirement: "one", Mandatory: true}
	b := policy("b", PhaseConstrain, 1)
	b.Add = &Obligation{Kind: "audit", Requirement: "two", Mandatory: true}
	out, err := Apply(p, []Policy{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan.Lifecycle != plan.LifecycleBlocked || len(out.Findings) != 1 || len(out.Plan.OpenQuestions) != 1 {
		t.Fatalf("conflict must block plan with finding and question: %+v", out)
	}
	if got := out.Plan.PassRecords[1].Outputs[0]; got != "conflict" {
		t.Fatalf("conflict history = %q", got)
	}
}

func TestMergePoliciesRequiresExplicitOverride(t *testing.T) {
	basePolicy := policy("default.audit", PhaseConstrain, 0)
	basePolicy.Add = &Obligation{Kind: "audit", Requirement: "emit event", Mandatory: true}
	project := policy("project.audit", PhaseConstrain, 0)
	project.Add = &Obligation{Kind: "audit", Requirement: "emit domain event", Mandatory: true}
	project.Supersedes = []string{"default.audit"}
	project.OverrideRationale = "The project event schema requires a domain event."
	merged, err := MergePolicies([]Policy{basePolicy}, []Policy{project})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || merged[0].ID != "project.audit" {
		t.Fatalf("unexpected merged policies: %+v", merged)
	}
	project.OverrideRationale = ""
	if _, err := MergePolicies([]Policy{basePolicy}, []Policy{project}); err == nil {
		t.Fatal("expected unexplained override rejection")
	}
}

func TestApplyRejectsUnknownPhaseAndDuplicatePolicies(t *testing.T) {
	p := base(t)
	bad := policy("bad", Phase("later"), 0)
	if _, err := Apply(p, []Policy{bad}); err == nil {
		t.Fatal("expected invalid phase rejection")
	}
	dup := policy("same", PhaseConstrain, 0)
	if _, err := Apply(p, []Policy{dup, dup}); err == nil {
		t.Fatal("expected duplicate policy rejection")
	}
}
