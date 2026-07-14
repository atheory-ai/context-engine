// Package mutation implements the first narrow semantic-development workflow:
// a resolved domain mutation plan is constrained, lowered, rendered, lifted,
// and reported without writing source or substrate state.
package mutation

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/lift"
	"github.com/atheory-ai/context-engine/internal/semantic/passes"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
)

// Observer is the source-lift boundary for this workflow. It makes coverage
// explicit, so a partial plugin walk cannot accidentally be reported accepted.
type Observer interface {
	Observe(context.Context, *recipe.ImplementationRecipe, string) (*lift.Unit, error)
}

type Workflow struct {
	Renderer recipe.Renderer
	Observer Observer
	Rules    iir.RulePack
	Profile  recipe.RendererProfile
	Policies []passes.Policy
}

type Status string

const (
	StatusAccepted    Status = "accepted"
	StatusConditional Status = "conditional"
	StatusRejected    Status = "rejected"
)

// Result carries every handoff in the semantic pipeline. It is deliberately a
// read-only artifact; callers decide separately whether to present or write
// the source candidate.
type Result struct {
	Status         Status                       `json:"status"`
	Plan           *plan.SemanticPlan           `json:"plan"`
	Recipe         *recipe.ImplementationRecipe `json:"recipe,omitempty"`
	Source         string                       `json:"source,omitempty"`
	Observed       *lift.Unit                   `json:"observed,omitempty"`
	Report         *iir.Report                  `json:"report,omitempty"`
	PolicyFindings []passes.Finding             `json:"policyFindings"`
	Diagnostics    []string                     `json:"diagnostics"`
}

// Execute applies policies, lowers only a resolved plan, renders the compact
// recipe, and compares observed source semantics. It never writes files,
// execution state, or the substrate.
func (w Workflow) Execute(ctx context.Context, source *plan.SemanticPlan) (*Result, error) {
	if source == nil {
		return nil, fmt.Errorf("mutation workflow: plan is required")
	}
	if w.Renderer == nil || w.Observer == nil {
		return nil, fmt.Errorf("mutation workflow: renderer and observer are required")
	}
	result := &Result{Plan: source, PolicyFindings: []passes.Finding{}, Diagnostics: []string{}}
	if len(w.Policies) > 0 {
		policyOut, err := passes.Apply(source, w.Policies)
		if err != nil {
			return nil, err
		}
		result.Plan, result.PolicyFindings = policyOut.Plan, policyOut.Findings
	}
	if result.Plan.Lifecycle != plan.LifecycleResolved {
		result.Status = StatusConditional
		result.Diagnostics = append(result.Diagnostics, "Plan is not resolved; rendering is blocked.")
		return result, nil
	}
	lowered, diagnostics, err := recipe.Lower(result.Plan, w.Profile)
	for _, diagnostic := range diagnostics {
		result.Diagnostics = append(result.Diagnostics, diagnostic.Message)
	}
	if err != nil {
		result.Status = StatusConditional
		return result, nil
	}
	result.Recipe = lowered
	if !w.Renderer.Supports(lowered) {
		return nil, fmt.Errorf("mutation workflow: renderer does not support recipe %s", lowered.ID)
	}
	rendered, err := w.Renderer.Render(ctx, lowered)
	if err != nil {
		return nil, fmt.Errorf("mutation workflow render: %w", err)
	}
	if rendered.RecipeID != lowered.ID {
		return nil, fmt.Errorf("mutation workflow: renderer returned source for a different recipe")
	}
	result.Source = rendered.Source
	observed, err := w.Observer.Observe(ctx, lowered, rendered.Source)
	if err != nil {
		return nil, fmt.Errorf("mutation workflow source lift: %w", err)
	}
	result.Observed = observed
	if observed == nil || !observed.CanSatisfyMandatory() {
		result.Status = StatusConditional
		result.Diagnostics = append(result.Diagnostics, "Source lift is partial or unsupported; it cannot prove mandatory obligations.")
		return result, nil
	}
	expected := effectiveIntent(result.Plan.Intent, lowered)
	result.Report = iir.Verify(expected, observed.Observed, w.Rules)
	missingEffects := missingRequiredEffects(lowered, observed.Observed)
	missingFailures := missingRequiredFailures(lowered, observed.Observed)
	for _, effect := range missingEffects {
		result.Diagnostics = append(result.Diagnostics, "Implement required effect "+effect+" from recipe "+lowered.ID+".")
	}
	for _, failure := range missingFailures {
		result.Diagnostics = append(result.Diagnostics, "Implement required failure behavior "+failure+" from recipe "+lowered.ID+".")
	}
	if result.Report.Status == iir.StatusPassed && len(errorFindings(result.PolicyFindings)) == 0 && len(missingEffects) == 0 && len(missingFailures) == 0 {
		result.Status = StatusAccepted
		return result, nil
	}
	result.Status = StatusRejected
	result.Diagnostics = append(result.Diagnostics, repairGuidance(result.Report, result.PolicyFindings)...)
	return result, nil
}

// Policies encodes the initial vertical-slice policies as declarative
// lowering inputs. Future plugin/project manifests feed the same pass API.
func Policies() []passes.Policy {
	return []passes.Policy{
		{ID: "semantic.mutation.audit", Version: "v1", Phase: passes.PhaseConstrain, Priority: 10, Severity: passes.SeverityError, When: passes.Selector{ClaimKinds: []string{"effect.mutation"}}, Add: &passes.Obligation{Kind: "audit", Requirement: "audit.publish", Mandatory: true}},
		{ID: "semantic.mutation.provider-failure", Version: "v1", Phase: passes.PhaseConstrain, Priority: 20, Severity: passes.SeverityError, When: passes.Selector{ClaimKinds: []string{"failure.propagated"}}, Add: &passes.Obligation{Kind: "failure.wrap", Requirement: "wrap provider error", Mandatory: true}},
	}
}

func errorFindings(findings []passes.Finding) []passes.Finding {
	out := []passes.Finding{}
	for _, finding := range findings {
		if finding.Severity == passes.SeverityError {
			out = append(out, finding)
		}
	}
	return out
}

func repairGuidance(report *iir.Report, findings []passes.Finding) []string {
	guidance := []string{}
	for _, finding := range findings {
		if finding.Repair != "" {
			guidance = append(guidance, finding.Repair)
		}
	}
	if report != nil {
		guidance = append(guidance, report.RepairTargets...)
	}
	return unique(guidance)
}

func effectiveIntent(intent *iir.FunctionIntent, lowered *recipe.ImplementationRecipe) *iir.FunctionIntent {
	effective := *intent
	effective.SideEffects = append([]iir.SideEffect{}, intent.SideEffects...)
	seen := map[string]bool{}
	for _, effect := range effective.SideEffects {
		seen[effect.Name] = true
	}
	for _, effect := range lowered.Effects {
		if effect.Required && !effect.Forbidden && !seen[effect.Name] {
			effective.SideEffects = append(effective.SideEffects, iir.SideEffect{Name: effect.Name, Kind: effect.Kind})
			seen[effect.Name] = true
		}
	}
	return &effective
}

func missingRequiredEffects(lowered *recipe.ImplementationRecipe, observed *iir.FunctionIntent) []string {
	seen := map[string]bool{}
	for _, effect := range observed.SideEffects {
		seen[effect.Name] = true
	}
	missing := []string{}
	for _, effect := range lowered.Effects {
		if effect.Required && !effect.Forbidden && !seen[effect.Name] {
			missing = append(missing, effect.Name)
		}
	}
	return unique(missing)
}

func missingRequiredFailures(lowered *recipe.ImplementationRecipe, observed *iir.FunctionIntent) []string {
	seen := map[string]bool{}
	for _, failure := range observed.FailureModes {
		seen[failure.Code] = true
	}
	missing := []string{}
	for _, failure := range lowered.Failures {
		if failure.Strategy != "policy" && !seen[failure.Code] {
			missing = append(missing, failure.Code)
		}
	}
	return unique(missing)
}

func unique(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
