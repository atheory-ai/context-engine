// Package resolve deterministically binds a SemanticPlan's open questions to
// graph candidates. It only reads the substrate and preserves ambiguity instead
// of selecting an architectural dependency by guesswork.
package resolve

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

const DefaultThreshold = 0.80

type Outcome string

const (
	OutcomeResolved     Outcome = "resolved"
	OutcomeAmbiguous    Outcome = "ambiguous"
	OutcomeMissing      Outcome = "missing"
	OutcomeIncompatible Outcome = "incompatible"
)

// Result is the externally consumable explanation of one resolution attempt.
type Result struct {
	RequirementID string           `json:"requirementId"`
	Candidates    []plan.Candidate `json:"candidates"`
	Selected      *plan.Candidate  `json:"selected,omitempty"`
	Outcome       Outcome          `json:"outcome"`
	Explanation   string           `json:"explanation"`
}

// Report pairs a newly derived plan revision with every deterministic decision
// made during resolution.
type Report struct {
	Plan    *plan.SemanticPlan `json:"plan"`
	Results []Result           `json:"results"`
}

// Reader is the intentionally narrow, read-only graph capability this pass
// needs. core.SubstrateReader satisfies it without expanding the dependency
// floor or granting the pass any write operation.
type Reader interface {
	GetNodeByCanonicalID(ctx context.Context, projectID core.ProjectID, canonicalID string) (*core.Node, error)
	GetNodesBySuffix(ctx context.Context, projectID core.ProjectID, suffix string, limit int) ([]core.Node, error)
	GetEdgesFrom(ctx context.Context, projectID core.ProjectID, nodeID core.NodeID) ([]core.EdgeWithWeight, error)
	GetNode(ctx context.Context, projectID core.ProjectID, nodeID core.NodeID) (*core.Node, error)
}

// Resolver resolves eligible open questions with deterministic graph evidence.
type Resolver struct {
	reader    Reader
	threshold float64
	limit     int
}

// New constructs a resolver. Threshold is clamped to the valid score range;
// zero selects DefaultThreshold.
func New(reader Reader, threshold, limit int) (*Resolver, error) {
	if reader == nil {
		return nil, fmt.Errorf("semantic resolution: reader is required")
	}
	if threshold == 0 {
		threshold = int(DefaultThreshold * 100)
	}
	if threshold < 1 || threshold > 100 {
		return nil, fmt.Errorf("semantic resolution: threshold must be 1 through 100")
	}
	if limit <= 0 {
		limit = 20
	}
	return &Resolver{reader: reader, threshold: float64(threshold) / 100, limit: limit}, nil
}

// Resolve returns an immutable plan revision. Questions that have no unique,
// threshold-satisfying candidate remain open with their full candidate set.
func (r *Resolver) Resolve(ctx context.Context, source *plan.SemanticPlan) (*Report, error) {
	if r == nil || r.reader == nil {
		return nil, fmt.Errorf("semantic resolution: resolver is not configured")
	}
	if source == nil {
		return nil, fmt.Errorf("semantic resolution: plan is required")
	}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	candidate := *source
	candidate.Bindings = append([]plan.SymbolBinding{}, source.Bindings...)
	candidate.Decisions = append([]plan.Decision{}, source.Decisions...)
	candidate.PassRecords = append([]plan.PassRecord{}, source.PassRecords...)
	candidate.Provenance = append([]plan.Evidence{}, source.Provenance...)
	candidate.OpenQuestions = make([]plan.OpenQuestion, 0, len(source.OpenQuestions))

	report := &Report{Results: make([]Result, 0, len(source.OpenQuestions))}
	for _, question := range source.OpenQuestions {
		result, err := r.resolveQuestion(ctx, source, question)
		if err != nil {
			return nil, err
		}
		report.Results = append(report.Results, result)
		if result.Outcome != OutcomeResolved {
			question.Candidates = result.Candidates
			candidate.OpenQuestions = append(candidate.OpenQuestions, question)
			continue
		}
		selected := *result.Selected
		candidate.Bindings = append(candidate.Bindings, plan.SymbolBinding{
			ID:          bindingID(question.ID),
			Role:        bindingRole(question.ID),
			NodeID:      selected.NodeID,
			CanonicalID: selected.CanonicalID,
			State:       plan.KnowledgeResolved,
			Evidence:    selected.Evidence,
		})
		candidate.Decisions = append(candidate.Decisions, plan.Decision{
			ID:         "decision-" + question.ID,
			QuestionID: question.ID,
			Value:      selected.CanonicalID,
			State:      plan.KnowledgeResolved,
			Evidence:   selected.Evidence,
		})
		candidate.Provenance = append(candidate.Provenance, plan.Evidence{
			ID:          "resolution-" + question.ID,
			Field:       "openQuestions." + question.ID,
			Source:      "graph",
			Producer:    "semantic.resolve",
			Confidence:  plan.ConfidenceHigh,
			Explanation: result.Explanation,
		})
	}
	candidate.PassRecords = append(candidate.PassRecords, plan.PassRecord{
		ID:      plan.StableRecordID("pass", source.ID, "resolve"),
		PassID:  "semantic.resolve",
		Version: "v1",
		Phase:   "resolve",
		Inputs:  []string{source.ID},
		Outputs: resultIDs(report.Results),
		Evidence: []plan.Evidence{{
			ID:          "resolution-run",
			Source:      "host",
			Producer:    "semantic.resolve",
			Confidence:  plan.ConfidenceHigh,
			Explanation: "Deterministic graph resolution pass.",
		}},
	})
	if hasBlocking(candidate.OpenQuestions) {
		candidate.Lifecycle = plan.LifecycleResolving
	} else {
		candidate.Lifecycle = plan.LifecycleResolved
	}
	next, err := plan.NewRevision(source, &candidate)
	if err != nil {
		return nil, err
	}
	report.Plan = next
	return report, nil
}

func (r *Resolver) resolveQuestion(ctx context.Context, semanticPlan *plan.SemanticPlan, question plan.OpenQuestion) (Result, error) {
	query := queryFor(question, semanticPlan.Unit)
	candidates, err := r.graphCandidates(ctx, semanticPlan, query)
	if err != nil {
		return Result{}, fmt.Errorf("semantic resolution %q: %w", question.ID, err)
	}
	if len(candidates) == 0 {
		return Result{RequirementID: question.ID, Candidates: []plan.Candidate{}, Outcome: OutcomeMissing, Explanation: "No graph candidate matched the required semantic role."}, nil
	}
	sortCandidates(candidates)
	best := candidates[0]
	if best.Score < r.threshold {
		return Result{RequirementID: question.ID, Candidates: candidates, Outcome: OutcomeIncompatible, Explanation: "Candidates exist but none satisfies the deterministic resolution threshold."}, nil
	}
	if len(candidates) > 1 && candidates[1].Score == best.Score {
		return Result{RequirementID: question.ID, Candidates: candidates, Outcome: OutcomeAmbiguous, Explanation: "Multiple candidates tie at the required resolution score."}, nil
	}
	return Result{RequirementID: question.ID, Candidates: candidates, Selected: &best, Outcome: OutcomeResolved, Explanation: "Selected the unique highest-scoring graph candidate."}, nil
}

func (r *Resolver) graphCandidates(ctx context.Context, semanticPlan *plan.SemanticPlan, query string) ([]plan.Candidate, error) {
	seen := map[core.NodeID]plan.Candidate{}
	add := func(node *core.Node, score float64, method string) {
		if node == nil {
			return
		}
		candidate := plan.Candidate{
			NodeID: node.ID, CanonicalID: node.CanonicalID, Score: score,
			Evidence: []plan.Evidence{{
				ID: "candidate-" + string(node.ID), Source: "graph", Producer: "semantic.resolve", Confidence: confidenceFor(score),
				NodeID: node.ID, Explanation: method + " match for " + query + ".",
			}},
		}
		if existing, ok := seen[node.ID]; !ok || candidate.Score > existing.Score {
			seen[node.ID] = candidate
		}
	}
	node, err := r.reader.GetNodeByCanonicalID(ctx, semanticPlan.ProjectID, query)
	if err != nil {
		return nil, err
	}
	add(node, 1, "canonical")
	if semanticPlan.Unit.NodeID != "" {
		edges, err := r.reader.GetEdgesFrom(ctx, semanticPlan.ProjectID, semanticPlan.Unit.NodeID)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			node, err := r.reader.GetNode(ctx, semanticPlan.ProjectID, edge.TargetID)
			if err != nil {
				return nil, err
			}
			if node != nil && strings.Contains(strings.ToLower(node.CanonicalID), strings.ToLower(query)) {
				add(node, 0.85, "relationship")
			}
		}
	}
	nodes, err := r.reader.GetNodesBySuffix(ctx, semanticPlan.ProjectID, query, r.limit)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		add(&node, 0.60, "suffix")
	}
	out := make([]plan.Candidate, 0, len(seen))
	for _, candidate := range seen {
		out = append(out, candidate)
	}
	return out, nil
}

func queryFor(question plan.OpenQuestion, unit plan.SemanticUnit) string {
	if question.ID == "target-symbol" {
		return strings.TrimPrefix(unit.CanonicalID, "requested.")
	}
	return strings.TrimPrefix(question.ID, "binding-")
}

func bindingID(questionID string) string {
	return "binding-" + strings.TrimPrefix(questionID, "binding-")
}

func bindingRole(questionID string) string {
	if questionID == "target-symbol" {
		return "target"
	}
	return strings.TrimPrefix(questionID, "binding-")
}

func resultIDs(results []Result) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.RequirementID+":"+string(result.Outcome))
	}
	return ids
}

func hasBlocking(questions []plan.OpenQuestion) bool {
	for _, question := range questions {
		if question.Blocking {
			return true
		}
	}
	return false
}

func sortCandidates(candidates []plan.Candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].CanonicalID != candidates[j].CanonicalID {
			return candidates[i].CanonicalID < candidates[j].CanonicalID
		}
		return candidates[i].NodeID < candidates[j].NodeID
	})
}

func confidenceFor(score float64) plan.Confidence {
	if score >= 0.85 {
		return plan.ConfidenceHigh
	}
	return plan.ConfidenceMedium
}
