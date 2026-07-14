package plan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
)

func testIntent(t *testing.T) *iir.FunctionIntent {
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
		t.Fatalf("load intent: %v", err)
	}
	return intent
}

func existingUnit() SemanticUnit {
	return SemanticUnit{
		ID:          "customer-update",
		NodeID:      "node-customer-update",
		CanonicalID: "CustomerService.updateCustomer",
		Scope:       "function",
		Language:    "typescript",
		SourceRefs:  []SourceRef{{Path: "src/customer.ts", StartByte: 12, EndByte: 96}},
	}
}

func evidence(id string) []Evidence {
	return []Evidence{{
		ID:          id,
		Source:      "graph",
		Producer:    "test",
		Confidence:  ConfidenceHigh,
		Explanation: "fixture evidence",
	}}
}

func TestNewPlan_ExistingAndProvisionalUnits(t *testing.T) {
	intent := testIntent(t)
	existing, err := NewPlan("project", existingUnit(), intent)
	if err != nil {
		t.Fatalf("new existing plan: %v", err)
	}
	if existing.ID == "" || existing.Revision != 1 || existing.Lifecycle != LifecycleDeclared {
		t.Fatalf("unexpected initial plan: %+v", existing)
	}
	if existing.Intent.Origin != iir.OriginDeclared || existing.Intent.Visibility != iir.VisibilityPublic {
		t.Fatalf("intent was not canonicalized: %+v", existing.Intent)
	}

	provisional := existingUnit()
	provisional.NodeID = ""
	provisional.CanonicalID = "requested.CustomerService.updateCustomer"
	created, err := NewPlan(core.ProjectID("project"), provisional, intent)
	if err != nil {
		t.Fatalf("new provisional plan: %v", err)
	}
	if created.Unit.NodeID != "" || created.Unit.CanonicalID == "" {
		t.Fatalf("provisional unit = %+v", created.Unit)
	}
}

func TestValidate_RejectsSemanticInvariantViolations(t *testing.T) {
	base, err := NewPlan("project", existingUnit(), testIntent(t))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		edit func(*SemanticPlan)
		want string
	}{
		{
			name: "inferred claim without evidence",
			edit: func(plan *SemanticPlan) {
				plan.Claims = []SemanticClaim{{ID: "claim", Kind: "effect", Statement: "writes customer", State: KnowledgeInferred}}
			},
			want: "claim claim evidence is required",
		},
		{
			name: "resolved with blocking question",
			edit: func(plan *SemanticPlan) {
				plan.Lifecycle = LifecycleResolved
				plan.OpenQuestions = []OpenQuestion{{ID: "repository", Prompt: "Which repository?", Blocking: true, State: KnowledgeUnknown, Evidence: evidence("repository-evidence")}}
			},
			want: "blocking open question",
		},
		{
			name: "decision cycle",
			edit: func(plan *SemanticPlan) {
				plan.Decisions = []Decision{
					{ID: "a", Value: "one", State: KnowledgeDeclared, DependsOn: []string{"b"}},
					{ID: "b", Value: "two", State: KnowledgeDeclared, DependsOn: []string{"a"}},
				}
			},
			want: "contain a cycle",
		},
		{
			name: "duplicate record id",
			edit: func(plan *SemanticPlan) {
				plan.Claims = []SemanticClaim{{ID: "same", Kind: "effect", Statement: "writes", State: KnowledgeDeclared}}
				plan.Obligations = []Obligation{{ID: "same", Kind: "audit", Requirement: "emit audit event", State: KnowledgeDeclared}}
			},
			want: "duplicate semantic record id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate, err := clone(base)
			if err != nil {
				t.Fatal(err)
			}
			test.edit(candidate)
			err = candidate.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMarshalCanonical_IsStableAcrossCollectionOrder(t *testing.T) {
	left, err := NewPlan("project", existingUnit(), testIntent(t))
	if err != nil {
		t.Fatal(err)
	}
	left.Claims = []SemanticClaim{
		{ID: "z-claim", Kind: "effect", Statement: "emit audit", State: KnowledgeObserved, Evidence: evidence("z-evidence")},
		{ID: "a-claim", Kind: "failure", Statement: "wrap provider error", State: KnowledgeDeclared},
	}
	left.Decisions = []Decision{
		{ID: "z-decision", Value: "event", State: KnowledgeDeclared, DependsOn: []string{}},
		{ID: "a-decision", Value: "repository", State: KnowledgeDeclared, DependsOn: []string{}},
	}
	right, err := clone(left)
	if err != nil {
		t.Fatal(err)
	}
	right.Claims[0], right.Claims[1] = right.Claims[1], right.Claims[0]
	right.Decisions[0], right.Decisions[1] = right.Decisions[1], right.Decisions[0]

	leftJSON, err := MarshalCanonical(left)
	if err != nil {
		t.Fatalf("marshal left: %v", err)
	}
	rightJSON, err := MarshalCanonical(right)
	if err != nil {
		t.Fatalf("marshal right: %v", err)
	}
	if !bytes.Equal(leftJSON, rightJSON) {
		t.Fatalf("canonical JSON differs\nleft:  %s\nright: %s", leftJSON, rightJSON)
	}
}

func TestParseJSON_StrictAndUpgradeable(t *testing.T) {
	plan, err := NewPlan("project", existingUnit(), testIntent(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalCanonical(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseJSON(raw); err != nil {
		t.Fatalf("parse canonical plan: %v", err)
	}
	upgraded, err := UpgradeJSON(raw)
	if err != nil {
		t.Fatalf("upgrade v1 plan: %v", err)
	}
	if !bytes.Equal(raw, upgraded) {
		t.Fatalf("upgrade changed canonical v1 plan\nwant: %s\n got: %s", raw, upgraded)
	}

	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	object["unexpected"] = true
	unknown, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseJSON(unknown); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("strict parse error = %v", err)
	}
	if _, err := ParseJSON(append(raw, []byte(` {}`)...)); err == nil {
		t.Fatalf("trailing JSON parse error = %v", err)
	}
}

func TestNewRevision_RetainsParentProvenanceAndChangesIdentity(t *testing.T) {
	parent, err := NewPlan("project", existingUnit(), testIntent(t))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := clone(parent)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Provenance = []Evidence{{
		ID:          "pass-evidence",
		Source:      "pass",
		Producer:    "semantic.resolve",
		Confidence:  ConfidenceHigh,
		Explanation: "Resolved repository candidate.",
	}}
	candidate.Claims = []SemanticClaim{{
		ID:        "repository-boundary",
		Kind:      "boundary",
		Statement: "uses customer repository",
		State:     KnowledgeResolved,
		Evidence:  evidence("repository-evidence"),
	}}
	next, err := NewRevision(parent, candidate)
	if err != nil {
		t.Fatalf("new revision: %v", err)
	}
	if next.ParentID != parent.ID || next.Revision != parent.Revision+1 || next.ID == parent.ID {
		t.Fatalf("unexpected revision: parent=%q revision=%d id=%q", next.ParentID, next.Revision, next.ID)
	}
	if len(next.Provenance) != 2 || next.Provenance[0].ID != "intent" || next.Provenance[1].ID != "pass-evidence" {
		t.Fatalf("parent provenance was not retained: %+v", next.Provenance)
	}
}

func TestStableRecordID_IsDeterministicAndValid(t *testing.T) {
	first := StableRecordID("binding", "project", "repository")
	second := StableRecordID("binding", "project", "repository")
	if first != second || !stableID.MatchString(first) {
		t.Fatalf("stable record ids = %q, %q", first, second)
	}
}
