// Package passes evaluates deterministic, declarative semantic policies over a
// plan. It has no substrate writer and cannot execute plugin-supplied code.
package passes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

// Phase identifies a deterministic point in semantic compilation.
type Phase string

const (
	PhaseResolve     Phase = "resolve"
	PhaseEnrich      Phase = "enrich"
	PhaseConstrain   Phase = "constrain"
	PhasePreGenerate Phase = "pre_generate"
	PhaseVerify      Phase = "verify"
	PhaseRepair      Phase = "repair_guidance"
)

// Severity communicates the actionability of a policy finding.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Finding struct {
	PolicyID string   `json:"policyId"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Repair   string   `json:"repair,omitempty"`
}

// Selector is an intentionally small, declarative applicability contract.
// Additional selector vocabulary can be introduced without allowing a policy
// contributor to execute arbitrary host code.
type Selector struct {
	ClaimKinds []string `json:"claimKinds,omitempty"`
	Languages  []string `json:"languages,omitempty"`
}

type Obligation struct {
	Kind        string `json:"kind"`
	Requirement string `json:"requirement"`
	Mandatory   bool   `json:"mandatory"`
}

// Policy is a host-evaluated contribution. Supersedes is only meaningful when
// contributions are merged by MergePolicies, where it explicitly replaces a
// lower-layer policy and must carry a human-readable rationale.
type Policy struct {
	ID                string      `json:"id"`
	Version           string      `json:"version"`
	Phase             Phase       `json:"phase"`
	Priority          int         `json:"priority"`
	When              Selector    `json:"when"`
	Add               *Obligation `json:"add,omitempty"`
	RequireApproval   bool        `json:"requireApproval,omitempty"`
	Severity          Severity    `json:"severity"`
	Supersedes        []string    `json:"supersedes,omitempty"`
	OverrideRationale string      `json:"overrideRationale,omitempty"`
}

type Output struct {
	Plan     *plan.SemanticPlan `json:"plan"`
	Findings []Finding          `json:"findings"`
}

// MergePolicies merges policy layers in caller order (for example built-in,
// plugin, then project). A later policy can remove a lower-layer policy only
// through an explicit, explained supersession; duplicate IDs are rejected.
func MergePolicies(layers ...[]Policy) ([]Policy, error) {
	merged := make([]Policy, 0)
	byID := make(map[string]int)
	for _, layer := range layers {
		for _, policy := range layer {
			if err := validatePolicy(policy); err != nil {
				return nil, err
			}
			if _, exists := byID[policy.ID]; exists {
				return nil, fmt.Errorf("semantic policy %q is defined more than once", policy.ID)
			}
			if len(policy.Supersedes) > 0 && strings.TrimSpace(policy.OverrideRationale) == "" {
				return nil, fmt.Errorf("semantic policy %q supersedes another policy without rationale", policy.ID)
			}
			for _, superseded := range policy.Supersedes {
				index, exists := byID[superseded]
				if !exists {
					return nil, fmt.Errorf("semantic policy %q supersedes unknown lower-layer policy %q", policy.ID, superseded)
				}
				merged = append(merged[:index], merged[index+1:]...)
				byID = policyIndexes(merged)
			}
			merged = append(merged, policy)
			byID[policy.ID] = len(merged) - 1
		}
	}
	return merged, nil
}

// Apply evaluates policies in compiler-phase, priority, ID order and returns
// an immutable revision. It only adds plan records; it has no substrate path.
func Apply(source *plan.SemanticPlan, policies []Policy) (*Output, error) {
	if source == nil {
		return nil, fmt.Errorf("semantic policy passes: plan is required")
	}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	sorted := append([]Policy{}, policies...)
	seen := make(map[string]struct{}, len(sorted))
	for _, policy := range sorted {
		if err := validatePolicy(policy); err != nil {
			return nil, err
		}
		if _, exists := seen[policy.ID]; exists {
			return nil, fmt.Errorf("semantic policy %q is defined more than once", policy.ID)
		}
		seen[policy.ID] = struct{}{}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if phaseOrder(sorted[i].Phase) != phaseOrder(sorted[j].Phase) {
			return phaseOrder(sorted[i].Phase) < phaseOrder(sorted[j].Phase)
		}
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].ID < sorted[j].ID
	})
	candidate := *source
	candidate.Obligations = append([]plan.Obligation{}, source.Obligations...)
	candidate.OpenQuestions = append([]plan.OpenQuestion{}, source.OpenQuestions...)
	candidate.PassRecords = append([]plan.PassRecord{}, source.PassRecords...)
	candidate.Provenance = append([]plan.Evidence{}, source.Provenance...)
	out := &Output{Findings: []Finding{}}
	blocked := false
	for _, policy := range sorted {
		outcome := "skipped"
		if !selects(source, policy.When) {
			candidate.PassRecords = append(candidate.PassRecords, passRecord(source, policy, outcome))
			continue
		}
		outcome = "applied"
		if policy.Add != nil {
			if conflict := conflicting(candidate.Obligations, *policy.Add); conflict != "" {
				blocked = true
				outcome = "conflict"
				out.Findings = append(out.Findings, Finding{PolicyID: policy.ID, Severity: SeverityError, Message: conflict, Repair: "Choose or explicitly supersede one mandatory obligation."})
				candidate.OpenQuestions = append(candidate.OpenQuestions, approvalQuestion(source, "conflict", conflict, policy))
				candidate.PassRecords = append(candidate.PassRecords, passRecord(source, policy, outcome))
				continue
			}
			candidate.Obligations = append(candidate.Obligations, plan.Obligation{
				ID:          plan.StableRecordID("obligation", source.ID, policy.ID, policy.Add.Kind),
				Kind:        policy.Add.Kind,
				Requirement: policy.Add.Requirement,
				State:       plan.KnowledgeDeclared,
				Evidence:    []plan.Evidence{policyEvidence(source, policy, "obligation", "Declarative policy obligation.")},
			})
		}
		if policy.RequireApproval {
			candidate.OpenQuestions = append(candidate.OpenQuestions, approvalQuestion(source, "approval", "Approve policy "+policy.ID+" before generation.", policy))
		}
		if len(policy.Supersedes) > 0 {
			out.Findings = append(out.Findings, Finding{PolicyID: policy.ID, Severity: SeverityInfo, Message: "Policy explicitly supersedes " + strings.Join(policy.Supersedes, ", ") + "."})
		}
		candidate.PassRecords = append(candidate.PassRecords, passRecord(source, policy, outcome))
	}
	if blocked {
		candidate.Lifecycle = plan.LifecycleBlocked
	} else if len(candidate.OpenQuestions) > 0 && candidate.Lifecycle == plan.LifecycleResolved {
		candidate.Lifecycle = plan.LifecycleResolving
	}
	next, err := plan.NewRevision(source, &candidate)
	if err != nil {
		return nil, err
	}
	out.Plan = next
	return out, nil
}

func phaseOrder(phase Phase) int {
	switch phase {
	case PhaseResolve:
		return 0
	case PhaseEnrich:
		return 1
	case PhaseConstrain:
		return 2
	case PhasePreGenerate:
		return 3
	case PhaseVerify:
		return 4
	case PhaseRepair:
		return 5
	default:
		return -1
	}
}

func selects(p *plan.SemanticPlan, selector Selector) bool {
	if len(selector.Languages) > 0 {
		matched := false
		for _, language := range selector.Languages {
			if language == p.Unit.Language {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(selector.ClaimKinds) == 0 {
		return true
	}
	for _, claim := range p.Claims {
		for _, kind := range selector.ClaimKinds {
			if claim.Kind == kind {
				return true
			}
		}
	}
	return false
}

func validatePolicy(policy Policy) error {
	if strings.TrimSpace(policy.ID) == "" || strings.TrimSpace(policy.Version) == "" {
		return fmt.Errorf("semantic policy requires id and version")
	}
	if phaseOrder(policy.Phase) < 0 {
		return fmt.Errorf("semantic policy %q has invalid phase %q", policy.ID, policy.Phase)
	}
	if policy.Severity != SeverityInfo && policy.Severity != SeverityWarning && policy.Severity != SeverityError {
		return fmt.Errorf("semantic policy %q has invalid severity %q", policy.ID, policy.Severity)
	}
	if policy.Add != nil && (strings.TrimSpace(policy.Add.Kind) == "" || strings.TrimSpace(policy.Add.Requirement) == "") {
		return fmt.Errorf("semantic policy %q has invalid obligation", policy.ID)
	}
	return nil
}

func conflicting(existing []plan.Obligation, add Obligation) string {
	if !add.Mandatory {
		return ""
	}
	for _, obligation := range existing {
		if obligation.Kind == add.Kind && obligation.Requirement != add.Requirement {
			return "mandatory policy conflict for " + add.Kind
		}
	}
	return ""
}

func approvalQuestion(source *plan.SemanticPlan, kind, message string, policy Policy) plan.OpenQuestion {
	return plan.OpenQuestion{
		ID:         plan.StableRecordID("question", source.ID, kind, policy.ID),
		Prompt:     message,
		Blocking:   true,
		State:      plan.KnowledgeUnknown,
		Evidence:   []plan.Evidence{policyEvidence(source, policy, kind, message)},
		Candidates: []plan.Candidate{},
	}
}

func passRecord(source *plan.SemanticPlan, policy Policy, outcome string) plan.PassRecord {
	return plan.PassRecord{
		ID:       plan.StableRecordID("pass", source.ID, policy.ID),
		PassID:   policy.ID,
		Version:  policy.Version,
		Phase:    string(policy.Phase),
		Inputs:   []string{source.ID},
		Outputs:  []string{outcome},
		Evidence: []plan.Evidence{policyEvidence(source, policy, "pass", "Declarative policy pass: "+outcome+".")},
	}
}

func policyEvidence(source *plan.SemanticPlan, policy Policy, field, explanation string) plan.Evidence {
	return plan.Evidence{
		ID:          plan.StableRecordID("evidence", source.ID, policy.ID, field),
		Source:      "policy",
		Producer:    policy.ID,
		Field:       field,
		Confidence:  plan.ConfidenceHigh,
		Explanation: explanation,
	}
}

func policyIndexes(policies []Policy) map[string]int {
	indexes := make(map[string]int, len(policies))
	for index, policy := range policies {
		indexes[policy.ID] = index
	}
	return indexes
}
