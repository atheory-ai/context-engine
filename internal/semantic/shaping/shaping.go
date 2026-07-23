// Package shaping turns declared or natural-language intent into canonical,
// provenance-aware SemanticPlan input. It is the model boundary for semantic
// planning; the plan package itself remains deterministic and model-free.
package shaping

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	iirshaper "github.com/atheory-ai/context-engine/internal/iir/shaper"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

// Requirement is an explicit caller-supplied fact needed to compile intent but
// not represented by FunctionIntent. Resolution answers it in a later pass;
// shaping only makes the missing decision visible.
type Requirement struct {
	ID       string
	Prompt   string
	Blocking bool
}

// Input is one declared or natural-language request to create an initial plan.
// Exactly one of Intent or Description is required.
type Input struct {
	ProjectID              core.ProjectID
	Unit                   plan.SemanticUnit
	Intent                 *iir.FunctionIntent
	Description            string
	RequiredBindings       []Requirement
	RequireFailureStrategy bool
}

// IntentShaper is the narrow model-facing contract supplied by the existing IIR
// shaper. It makes semantic shaping deterministic to test after this boundary.
type IntentShaper interface {
	Shape(ctx context.Context, description string) (*iir.FunctionIntent, error)
}

// CandidateIntentShaper is implemented by the built-in model shaper and can be
// implemented by a harness adapter. It carries model-identified uncertainty in
// addition to the valid IIR node.
type CandidateIntentShaper interface {
	IntentShaper
	ShapeCandidate(ctx context.Context, description string) (*iirshaper.Candidate, error)
}

// LanguageCandidateIntentShaper adds the resolved target language to the
// model task. The built-in shaper implements it; older/harness adapters remain
// source-compatible through CandidateIntentShaper.
type LanguageCandidateIntentShaper interface {
	CandidateIntentShaper
	ShapeCandidateForLanguage(ctx context.Context, description, language string) (*iirshaper.Candidate, error)
}

// Planner produces initial SemanticPlan revisions.
type Planner struct {
	shaper IntentShaper
}

// New constructs a planner backed by the existing validated IIR shaper.
func New(provider core.LLMProvider) *Planner {
	return NewWithShaper(iirshaper.New(provider))
}

// NewWithShaper supplies a shaper implementation, primarily for deterministic
// tests and alternate approved model boundaries.
func NewWithShaper(shaper IntentShaper) *Planner {
	return &Planner{shaper: shaper}
}

// FromIntent turns a declared FunctionIntent into an initial SemanticPlan. It
// never calls a model.
func (p *Planner) FromIntent(input Input) (*plan.SemanticPlan, error) {
	if input.Intent == nil {
		return nil, fmt.Errorf("semantic shaping: declared intent is required")
	}
	if strings.TrimSpace(input.Description) != "" {
		return nil, fmt.Errorf("semantic shaping: provide declared intent or description, not both")
	}
	return p.build(input, input.Intent, false)
}

// Shape turns natural language into a validated inferred intent through the
// existing IIR shaper, then immediately makes it canonical plan input.
func (p *Planner) Shape(ctx context.Context, input Input) (*plan.SemanticPlan, error) {
	if input.Intent != nil {
		return nil, fmt.Errorf("semantic shaping: provide declared intent or description, not both")
	}
	if strings.TrimSpace(input.Description) == "" {
		return nil, fmt.Errorf("semantic shaping: description is required")
	}
	if p == nil || p.shaper == nil {
		return nil, fmt.Errorf("semantic shaping: no intent shaper configured")
	}
	var candidateIntent *iir.FunctionIntent
	var candidateQuestions []iirshaper.OpenQuestion
	var candidateTags []string
	if candidateShaper, ok := p.shaper.(CandidateIntentShaper); ok {
		var candidate *iirshaper.Candidate
		var err error
		if languageShaper, ok := candidateShaper.(LanguageCandidateIntentShaper); ok {
			candidate, err = languageShaper.ShapeCandidateForLanguage(ctx, input.Description, input.Unit.Language)
		} else {
			candidate, err = candidateShaper.ShapeCandidate(ctx, input.Description)
		}
		if err != nil {
			return nil, fmt.Errorf("semantic shaping: shape intent: %w", err)
		}
		if candidate == nil || candidate.Intent == nil {
			return nil, fmt.Errorf("semantic shaping: intent shaper returned no intent")
		}
		candidateIntent, candidateQuestions, candidateTags = candidate.Intent, candidate.OpenQuestions, candidate.SemanticTags
	} else {
		intent, err := p.shaper.Shape(ctx, input.Description)
		if err != nil {
			return nil, fmt.Errorf("semantic shaping: shape intent: %w", err)
		}
		candidateIntent = intent
	}
	if candidateIntent == nil {
		return nil, fmt.Errorf("semantic shaping: intent shaper returned no intent")
	}
	semanticPlan, err := p.build(input, candidateIntent, true)
	if err != nil {
		return nil, err
	}
	semanticPlan, err = attachCandidateTags(semanticPlan, candidateTags)
	if err != nil {
		return nil, err
	}
	return attachCandidateQuestions(semanticPlan, candidateQuestions)
}

// attachCandidateTags records controlled model classifications as inferred
// claims. A policy can require several tags, so an uncertain model label never
// becomes an unconditional framework or security obligation.
func attachCandidateTags(source *plan.SemanticPlan, tags []string) (*plan.SemanticPlan, error) {
	if len(tags) == 0 {
		return source, nil
	}
	candidate := *source
	candidate.Claims = append([]plan.SemanticClaim{}, source.Claims...)
	candidate.PassRecords = append([]plan.PassRecord{}, source.PassRecords...)
	for _, tag := range tags {
		id := plan.StableRecordID("claim", "model", tag)
		candidate.Claims = append(candidate.Claims, plan.SemanticClaim{
			ID: id, Kind: tag, Statement: tag, State: plan.KnowledgeInferred,
			Evidence: []plan.Evidence{{ID: id + ".evidence", Field: "semanticTags", Source: "model", Producer: "semantic.shaping", Confidence: plan.ConfidenceMedium, Explanation: "Model-proposed controlled semantic tag."}},
		})
	}
	candidate.PassRecords = append(candidate.PassRecords, plan.PassRecord{
		ID: plan.StableRecordID("pass", source.ID, "model-semantic-tags"), PassID: "semantic.shaping.tags", Version: "v1", Phase: "resolve", Inputs: []string{source.ID}, Outputs: append([]string(nil), tags...),
		Evidence: []plan.Evidence{{ID: plan.StableRecordID("evidence", source.ID, "model-semantic-tags"), Source: "model", Producer: "semantic.shaping", Confidence: plan.ConfidenceMedium, Explanation: "Validated model semantic tags attached to plan."}},
	})
	return plan.NewRevision(source, &candidate)
}

func attachCandidateQuestions(source *plan.SemanticPlan, questions []iirshaper.OpenQuestion) (*plan.SemanticPlan, error) {
	if len(questions) == 0 {
		return source, nil
	}
	candidate := *source
	candidate.OpenQuestions = append([]plan.OpenQuestion{}, source.OpenQuestions...)
	candidate.PassRecords = append([]plan.PassRecord{}, source.PassRecords...)
	candidate.Provenance = append([]plan.Evidence{}, source.Provenance...)
	unknownFields := make(map[string]struct{}, len(questions))
	for _, question := range questions {
		field := strings.TrimSpace(question.Field)
		if field == "" {
			continue
		}
		// FunctionIntent has a few historical defaults (notably public
		// visibility). A model question wins over such a default: do not record
		// the defaulted field as a model assertion.
		root := strings.Split(field, ".")[0]
		unknownFields["intent."+root] = struct{}{}
	}
	if len(unknownFields) > 0 {
		provenance := candidate.Provenance[:0]
		for _, evidence := range candidate.Provenance {
			if _, unknown := unknownFields[evidence.Field]; unknown {
				continue
			}
			provenance = append(provenance, evidence)
		}
		candidate.Provenance = provenance
	}
	for index, question := range questions {
		field := strings.TrimSpace(question.Field)
		if field == "" {
			field = fmt.Sprintf("model-question-%d", index+1)
		}
		id := plan.StableRecordID("question", "model", field, question.Prompt)
		candidate.OpenQuestions = append(candidate.OpenQuestions, plan.OpenQuestion{
			ID: id, Prompt: question.Prompt, Blocking: question.Blocking, State: plan.KnowledgeUnknown,
			Evidence:   []plan.Evidence{{ID: id + ".evidence", Field: field, Source: "model", Producer: "semantic.shaping", Confidence: plan.ConfidenceMedium, Explanation: "Model identified a missing decision rather than inventing it."}},
			Candidates: []plan.Candidate{},
		})
	}
	candidate.PassRecords = append(candidate.PassRecords, plan.PassRecord{
		ID: plan.StableRecordID("pass", source.ID, "model-open-questions"), PassID: "semantic.shaping.questions", Version: "v1", Phase: "resolve", Inputs: []string{source.ID}, Outputs: []string{"open questions"},
		Evidence: []plan.Evidence{{ID: plan.StableRecordID("evidence", source.ID, "model-open-questions"), Source: "model", Producer: "semantic.shaping", Confidence: plan.ConfidenceMedium, Explanation: "Validated model open questions attached to plan."}},
	})
	return plan.NewRevision(source, &candidate)
}

func (p *Planner) build(input Input, intent *iir.FunctionIntent, inferred bool) (*plan.SemanticPlan, error) {
	unit, provisional := normalizeUnit(input.Unit, intent)
	semanticPlan, err := plan.NewPlan(input.ProjectID, unit, intent)
	if err != nil {
		return nil, err
	}
	if inferred {
		semanticPlan.Provenance = append(semanticPlan.Provenance, fieldEvidence(intent)...)
	}
	semanticPlan.OpenQuestions = append(semanticPlan.OpenQuestions, openQuestions(input, intent, provisional, inferred)...)
	if err := semanticPlan.Validate(); err != nil {
		return nil, err
	}
	return semanticPlan, nil
}

func normalizeUnit(unit plan.SemanticUnit, intent *iir.FunctionIntent) (plan.SemanticUnit, bool) {
	provisional := unit.NodeID == "" && strings.TrimSpace(unit.CanonicalID) == ""
	if unit.ID == "" {
		unit.ID = plan.StableRecordID("unit", intent.Language, intent.Name)
	}
	if unit.CanonicalID == "" && unit.NodeID == "" {
		unit.CanonicalID = "requested." + intent.Name
	}
	if unit.Scope == "" {
		unit.Scope = "function"
	}
	if unit.Language == "" {
		unit.Language = intent.Language
	}
	if unit.SourceRefs == nil {
		unit.SourceRefs = []plan.SourceRef{}
	}
	return unit, provisional
}

func openQuestions(input Input, intent *iir.FunctionIntent, provisional, inferred bool) []plan.OpenQuestion {
	source, producer := "user", "semantic.shaping"
	if inferred {
		source, producer = "model", "semantic.shaping"
	}
	evidence := func(id, field, explanation string) []plan.Evidence {
		return []plan.Evidence{{
			ID:          id + "-evidence",
			Field:       field,
			Source:      source,
			Producer:    producer,
			Confidence:  confidence(inferred),
			Explanation: explanation,
		}}
	}
	questions := []plan.OpenQuestion{}
	if provisional {
		questions = append(questions, plan.OpenQuestion{
			ID:       "target-symbol",
			Prompt:   "Which existing symbol should this change target, or should it create a new symbol?",
			Blocking: true,
			State:    plan.KnowledgeUnknown,
			Evidence: evidence("target-symbol", "unit", "No existing target symbol was supplied."),
		})
	}
	for _, requirement := range input.RequiredBindings {
		id := requirement.ID
		if id == "" {
			id = plan.StableRecordID("binding", requirement.Prompt)
		}
		prompt := requirement.Prompt
		if prompt == "" {
			prompt = "Which symbol satisfies the required " + id + " role?"
		}
		questions = append(questions, plan.OpenQuestion{
			ID:       "binding-" + id,
			Prompt:   prompt,
			Blocking: requirement.Blocking,
			State:    plan.KnowledgeUnknown,
			Evidence: evidence("binding-"+id, "binding", "A required binding has not yet been resolved."),
		})
	}
	if input.RequireFailureStrategy && len(intent.FailureModes) == 0 {
		questions = append(questions, plan.OpenQuestion{
			ID:       "failure-strategy",
			Prompt:   "How should provider or domain failures be represented and propagated?",
			Blocking: true,
			State:    plan.KnowledgeUnknown,
			Evidence: evidence("failure-strategy", "failureModes", "The request requires a failure strategy but none was declared."),
		})
	}
	return questions
}

func fieldEvidence(intent *iir.FunctionIntent) []plan.Evidence {
	fields := []string{"name", "language", "inputs", "returns", "behavior", "sideEffects", "failureModes", "constraints"}
	evidence := make([]plan.Evidence, 0, len(fields))
	for _, field := range fields {
		evidence = append(evidence, plan.Evidence{
			ID:          "intent-" + field,
			Field:       "intent." + field,
			Source:      "model",
			Producer:    "semantic.shaping",
			Confidence:  plan.ConfidenceMedium,
			Explanation: "Model-inferred " + field + " for " + intent.Name + ".",
		})
	}
	return evidence
}

func confidence(inferred bool) plan.Confidence {
	if inferred {
		return plan.ConfidenceMedium
	}
	return plan.ConfidenceHigh
}
