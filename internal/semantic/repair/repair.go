// Package repair turns evidence-backed semantic verification findings into
// minimal plan or recipe deltas. It never rewrites source and never mutates a
// semantic plan revision.
package repair

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atheory-ai/context-engine/internal/semantic/lift"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
	semanticverify "github.com/atheory-ai/context-engine/internal/semantic/verify"
)

type Status string

const (
	StatusProposed  Status = "proposed"
	StatusApproved  Status = "approved"
	StatusApplied   Status = "applied"
	StatusRejected  Status = "rejected"
	StatusExhausted Status = "exhausted"
)

type Classification string

const (
	ImplementationDivergence Classification = "implementation_divergence"
	ResolutionPolicyDefect   Classification = "resolution_policy_defect"
	InsufficientEvidence     Classification = "insufficient_evidence"
	UserDecisionRequired     Classification = "user_decision_required"
)

type Artifact struct {
	ID         string `json:"id"`
	SourceHash string `json:"sourceHash"`
	RendererID string `json:"rendererId,omitempty"`
}

type Change struct {
	Kind         string `json:"kind"` // recipe_patch | plan_question
	Operation    string `json:"operation"`
	TargetID     string `json:"targetId"`
	Requirement  string `json:"requirement"`
	FindingIndex int    `json:"findingIndex"`
}

// Plan links every repair to the exact plan, recipe, source candidate, lift,
// and verification report that caused it.
type Plan struct {
	ID                    string         `json:"id"`
	ParentPlanRevision    string         `json:"parentPlanRevision"`
	RecipeID              string         `json:"recipeId"`
	Artifact              Artifact       `json:"artifact"`
	ObservedNodeID        string         `json:"observedNodeId,omitempty"`
	VerificationID        string         `json:"verificationId"`
	Classification        Classification `json:"classification"`
	Changes               []Change       `json:"changes"`
	Rationale             []string       `json:"rationale"`
	AffectedClaimIDs      []string       `json:"affectedClaimIds"`
	AffectedObligationIDs []string       `json:"affectedObligationIds"`
	Status                Status         `json:"status"`
}

// Propose classifies verification output and returns the smallest semantic
// change set. A violated recipe fact yields a recipe patch; ambiguous, blocked,
// or unsupported facts yield a question instead of a retry instruction.
func Propose(source *plan.SemanticPlan, lowered *recipe.ImplementationRecipe, artifact Artifact, observed *lift.Unit, report *semanticverify.Report, previous []Plan) (*Plan, error) {
	if source == nil || lowered == nil || report == nil {
		return nil, fmt.Errorf("semantic repair: plan, recipe, and verification report are required")
	}
	if lowered.PlanRevisionID != source.ID || report.PlanRevisionID != source.ID || report.RecipeID != lowered.ID {
		return nil, fmt.Errorf("semantic repair: inputs do not share a plan and recipe revision")
	}
	if artifact.ID == "" || artifact.SourceHash == "" {
		return nil, fmt.Errorf("semantic repair: artifact id and source hash are required")
	}
	repair := &Plan{ParentPlanRevision: source.ID, RecipeID: lowered.ID, Artifact: artifact, VerificationID: verificationID(report), Changes: []Change{}, Rationale: []string{}, AffectedClaimIDs: []string{}, AffectedObligationIDs: []string{}, Status: StatusProposed}
	if observed != nil {
		repair.ObservedNodeID = string(observed.NodeID)
	}
	if source.Lifecycle == plan.LifecycleBlocked {
		repair.Classification = UserDecisionRequired
		repair.Changes = append(repair.Changes, Change{Kind: "plan_question", Operation: "resolve_conflict", TargetID: source.ID, Requirement: "Resolve the blocked policy or resolution conflict before rendering.", FindingIndex: -1})
		repair.Rationale = append(repair.Rationale, "The semantic plan is blocked.")
	} else if report.Status == semanticverify.StatusInconclusive {
		repair.Classification = InsufficientEvidence
		repair.Rationale = append(repair.Rationale, "Verification is inconclusive; source repair would fabricate evidence.")
	} else {
		classifyFindings(repair, report)
	}
	if repair.Classification == "" {
		repair.Classification = ImplementationDivergence
	}
	canonicalize(repair)
	repair.ID = makeID(repair)
	for _, prior := range previous {
		if prior.ID == repair.ID {
			repair.Status = StatusExhausted
			repair.Rationale = append(repair.Rationale, "An equivalent repair plan was already attempted.")
			break
		}
	}
	return repair, nil
}

func classifyFindings(repair *Plan, report *semanticverify.Report) {
	for index, finding := range report.Findings {
		switch finding.Result {
		case semanticverify.ResultViolated:
			repair.Classification = ImplementationDivergence
			repair.Changes = append(repair.Changes, Change{Kind: "recipe_patch", Operation: patchOperation(finding.Expected), TargetID: finding.PlanRecordID, Requirement: finding.Expected, FindingIndex: index})
			repair.Rationale = append(repair.Rationale, finding.RepairTarget)
			addAffected(repair, finding.PlanRecordID)
		case semanticverify.ResultUnsupported, semanticverify.ResultUnknown, semanticverify.ResultConditional:
			if repair.Classification == "" {
				repair.Classification = InsufficientEvidence
			}
			repair.Changes = append(repair.Changes, Change{Kind: "plan_question", Operation: "supply_evidence", TargetID: finding.PlanRecordID, Requirement: finding.Expected, FindingIndex: index})
			repair.Rationale = append(repair.Rationale, finding.RepairTarget)
			addAffected(repair, finding.PlanRecordID)
		}
	}
	if len(repair.Changes) == 0 {
		repair.Classification = InsufficientEvidence
		repair.Rationale = append(repair.Rationale, "No actionable semantic finding was available.")
	}
}

func patchOperation(expected string) string {
	if strings.Contains(expected, "effect ") {
		return "ensure_effect"
	}
	if strings.Contains(expected, "failure ") {
		return "ensure_failure"
	}
	return "satisfy_requirement"
}
func addAffected(repair *Plan, id string) {
	if strings.Contains(id, "obligation") {
		repair.AffectedObligationIDs = append(repair.AffectedObligationIDs, id)
	} else {
		repair.AffectedClaimIDs = append(repair.AffectedClaimIDs, id)
	}
}

func verificationID(report *semanticverify.Report) string {
	payload, _ := json.Marshal(report)
	sum := sha256.Sum256(payload)
	return "verification-" + hex.EncodeToString(sum[:12])
}
func makeID(repair *Plan) string {
	copy := *repair
	copy.ID = ""
	copy.Status = StatusProposed
	payload, _ := json.Marshal(copy)
	sum := sha256.Sum256(payload)
	return "repair-" + hex.EncodeToString(sum[:16])
}
func canonicalize(repair *Plan) {
	sort.Slice(repair.Changes, func(i, j int) bool { return repair.Changes[i].FindingIndex < repair.Changes[j].FindingIndex })
	sort.Strings(repair.Rationale)
	repair.Rationale = unique(repair.Rationale)
	sort.Strings(repair.AffectedClaimIDs)
	repair.AffectedClaimIDs = unique(repair.AffectedClaimIDs)
	sort.Strings(repair.AffectedObligationIDs)
	repair.AffectedObligationIDs = unique(repair.AffectedObligationIDs)
}
func unique(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
