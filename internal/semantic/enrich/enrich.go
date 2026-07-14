// Package enrich derives evidence-bearing semantic claims from an already
// resolved plan. It describes the codebase; policy passes decide what should be
// required of it.
package enrich

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

// Reader is the minimal graph view required for bounded structural enrichment.
type Reader interface {
	GetEdgesFrom(context.Context, core.ProjectID, core.NodeID) ([]core.EdgeWithWeight, error)
	GetNode(context.Context, core.ProjectID, core.NodeID) (*core.Node, error)
	GetCallers(context.Context, core.ProjectID, core.NodeID, int) ([]core.NodeWithActivation, error)
}

// Observations supplies already-lifted IIR for a symbol. It is deliberately
// separate from Reader because the graph's IIR storage is a distinct concern.
type Observations interface {
	ObservedIntent(context.Context, core.ProjectID, core.NodeID) (*iir.FunctionIntent, error)
}

type Enricher struct {
	reader       Reader
	observations Observations
	callerDepth  int
}

func New(reader Reader, observations Observations, callerDepth int) (*Enricher, error) {
	if reader == nil {
		return nil, fmt.Errorf("semantic enrichment: reader is required")
	}
	if callerDepth <= 0 {
		callerDepth = 1
	}
	return &Enricher{reader: reader, observations: observations, callerDepth: callerDepth}, nil
}

// Enrich returns an immutable plan revision. It never writes the substrate and
// every derived claim retains the graph/lift evidence that supports it.
func (e *Enricher) Enrich(ctx context.Context, source *plan.SemanticPlan) (*plan.SemanticPlan, error) {
	if e == nil || e.reader == nil {
		return nil, fmt.Errorf("semantic enrichment: enricher is not configured")
	}
	if source == nil {
		return nil, fmt.Errorf("semantic enrichment: plan is required")
	}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	candidate := *source
	candidate.Claims = append([]plan.SemanticClaim{}, source.Claims...)
	candidate.PassRecords = append([]plan.PassRecord{}, source.PassRecords...)
	candidate.Provenance = append([]plan.Evidence{}, source.Provenance...)
	if source.Unit.NodeID == "" {
		candidate.Claims = append(candidate.Claims, unknownClaim("source-unit", "No graph node is bound to this semantic unit; structural enrichment is unavailable."))
	} else {
		if err := e.addStructuralClaims(ctx, &candidate); err != nil {
			return nil, err
		}
		if err := e.addCallerClaims(ctx, &candidate); err != nil {
			return nil, err
		}
		if err := e.addObservedClaims(ctx, &candidate); err != nil {
			return nil, err
		}
	}
	candidate.PassRecords = append(candidate.PassRecords, plan.PassRecord{
		ID:       plan.StableRecordID("pass", source.ID, "enrich"),
		PassID:   "semantic.enrich",
		Version:  "v1",
		Phase:    "enrich",
		Inputs:   []string{source.ID},
		Outputs:  []string{"semantic claims"},
		Evidence: []plan.Evidence{{ID: "enrichment-run", Source: "host", Producer: "semantic.enrich", Confidence: plan.ConfidenceHigh, Explanation: "Bounded deterministic enrichment pass."}},
	})
	return plan.NewRevision(source, &candidate)
}

func (e *Enricher) addStructuralClaims(ctx context.Context, candidate *plan.SemanticPlan) error {
	edges, err := e.reader.GetEdgesFrom(ctx, candidate.ProjectID, candidate.Unit.NodeID)
	if err != nil {
		return fmt.Errorf("semantic enrichment dependencies: %w", err)
	}
	if len(edges) == 0 {
		candidate.Claims = append(candidate.Claims, unknownClaim("direct-dependencies", "No direct dependency edges were available from the resolved unit."))
		return nil
	}
	for _, edge := range edges {
		node, err := e.reader.GetNode(ctx, candidate.ProjectID, edge.TargetID)
		if err != nil {
			return err
		}
		if node == nil {
			candidate.Claims = append(candidate.Claims, unknownClaim("dependency-"+string(edge.TargetID), "A dependency edge points to an unavailable graph node."))
			continue
		}
		kind := "dependency"
		if boundary := boundaryKind(node.CanonicalID); boundary != "" {
			kind = "boundary." + boundary
		}
		candidate.Claims = append(candidate.Claims, observedClaim(kind, node.CanonicalID+" is a direct dependency.", node.ID, "structural graph dependency"))
	}
	return nil
}

func (e *Enricher) addCallerClaims(ctx context.Context, candidate *plan.SemanticPlan) error {
	callers, err := e.reader.GetCallers(ctx, candidate.ProjectID, candidate.Unit.NodeID, e.callerDepth)
	if err != nil {
		return fmt.Errorf("semantic enrichment callers: %w", err)
	}
	if len(callers) == 0 {
		return nil
	}
	for _, caller := range callers {
		candidate.Claims = append(candidate.Claims, observedClaim("caller", caller.CanonicalID+" calls this unit.", caller.ID, "bounded caller traversal"))
	}
	return nil
}

func (e *Enricher) addObservedClaims(ctx context.Context, candidate *plan.SemanticPlan) error {
	if e.observations == nil {
		candidate.Claims = append(candidate.Claims, unknownClaim("observed-intent", "No observed IIR lookup is configured."))
		return nil
	}
	intent, err := e.observations.ObservedIntent(ctx, candidate.ProjectID, candidate.Unit.NodeID)
	if err != nil {
		return fmt.Errorf("semantic enrichment observed intent: %w", err)
	}
	if intent == nil {
		candidate.Claims = append(candidate.Claims, unknownClaim("observed-intent", "No observed IIR is available for the resolved unit."))
		return nil
	}
	for _, effect := range intent.SideEffects {
		kind, basis := effect.Kind, effect.Basis
		if kind == "" || basis == "" {
			kind, basis = iir.ClassifyEffect(effect.Name)
		}
		state, confidence := plan.KnowledgeObserved, plan.ConfidenceHigh
		if basis == iir.BasisHeuristic {
			state, confidence = plan.KnowledgeInferred, plan.ConfidenceMedium
		}
		candidate.Claims = append(candidate.Claims, plan.SemanticClaim{
			ID: plan.StableRecordID("effect", effect.Name, basis), Kind: "effect." + kind, Statement: effect.Name,
			State: state, Evidence: []plan.Evidence{{ID: plan.StableRecordID("evidence", effect.Name, basis), Source: "semantic", Producer: "semantic.enrich", Confidence: confidence, Explanation: basis + " effect classifier basis."}},
		})
	}
	for _, failure := range intent.FailureModes {
		candidate.Claims = append(candidate.Claims, observedClaim("failure."+failure.Kind, failure.Code, candidate.Unit.NodeID, "observed IIR failure mode"))
	}
	return nil
}

func observedClaim(kind, statement string, nodeID core.NodeID, explanation string) plan.SemanticClaim {
	return plan.SemanticClaim{
		ID: plan.StableRecordID("claim", kind, statement, string(nodeID)), Kind: kind, Statement: statement, State: plan.KnowledgeObserved,
		Evidence: []plan.Evidence{{ID: plan.StableRecordID("evidence", kind, statement, string(nodeID)), Source: "structural", Producer: "semantic.enrich", NodeID: nodeID, Confidence: plan.ConfidenceHigh, Explanation: explanation}},
	}
}

func unknownClaim(key, explanation string) plan.SemanticClaim {
	return plan.SemanticClaim{
		ID: plan.StableRecordID("unknown", key), Kind: "unknown", Statement: explanation, State: plan.KnowledgeUnknown,
		Evidence: []plan.Evidence{{ID: plan.StableRecordID("evidence", key), Source: "unknown", Producer: "semantic.enrich", Confidence: plan.ConfidenceLow, Explanation: explanation}},
	}
}

func boundaryKind(canonicalID string) string {
	lower := strings.ToLower(canonicalID)
	switch {
	case strings.Contains(lower, "repository"):
		return "repository"
	case strings.Contains(lower, "audit") || strings.Contains(lower, "event") || strings.Contains(lower, "publisher"):
		return "publisher"
	case strings.Contains(lower, "provider") || strings.Contains(lower, "client"):
		return "provider"
	default:
		return ""
	}
}
