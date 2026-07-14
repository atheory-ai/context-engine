// Package verify evaluates a semantic plan and implementation recipe against a
// coverage-aware observed source lift. It never upgrades missing evidence to a
// passing result.
package verify

import (
	"fmt"

	"github.com/atheory-ai/context-engine/internal/semantic/lift"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
)

type Result string

const (
	ResultVerified    Result = "verified"
	ResultViolated    Result = "violated"
	ResultConditional Result = "conditional"
	ResultUnknown     Result = "unknown"
	ResultUnsupported Result = "unsupported"
)

type Status string

const (
	StatusPassed       Status = "passed"
	StatusFailed       Status = "failed"
	StatusInconclusive Status = "inconclusive"
)

type Finding struct {
	PlanRecordID string          `json:"planRecordId"`
	RecipeID     string          `json:"recipeId"`
	Result       Result          `json:"result"`
	Severity     string          `json:"severity"`
	Expected     string          `json:"expected"`
	Observed     string          `json:"observed,omitempty"`
	Evidence     []lift.Evidence `json:"evidence"`
	RepairTarget string          `json:"repairTarget,omitempty"`
}

type Report struct {
	Status          Status        `json:"status"`
	PlanRevisionID  string        `json:"planRevisionId"`
	RecipeID        string        `json:"recipeId"`
	Coverage        lift.Coverage `json:"coverage"`
	Findings        []Finding     `json:"findings"`
	TraversalCutoff bool          `json:"traversalCutoff"`
}

// Verify checks only facts exposed by the bounded v1 lift. It has no graph
// traversal; callers may set traversal behavior in later passes, which must
// mark a cutoff instead of claiming whole-program coverage.
func Verify(source *plan.SemanticPlan, lowered *recipe.ImplementationRecipe, observed *lift.Unit) (*Report, error) {
	if source == nil || lowered == nil {
		return nil, fmt.Errorf("semantic verification: plan and recipe are required")
	}
	if lowered.PlanRevisionID != source.ID {
		return nil, fmt.Errorf("semantic verification: recipe does not belong to plan revision")
	}
	report := &Report{Status: StatusPassed, PlanRevisionID: source.ID, RecipeID: lowered.ID, Findings: []Finding{}, Coverage: lift.CoverageUnsupported}
	if observed == nil {
		report.Status = StatusInconclusive
		report.Findings = append(report.Findings, unknownFinding(source.ID, lowered.ID, "observed source lift", nil))
		return report, nil
	}
	report.Coverage = observed.Coverage
	if !observed.CanSatisfyMandatory() {
		report.Status = StatusInconclusive
		for _, effect := range lowered.Effects {
			if effect.Required {
				report.Findings = append(report.Findings, conditionalFinding(effect.PlanRecordID, lowered.ID, "required effect "+effect.Name, observed.Evidence))
			}
		}
		for _, failure := range lowered.Failures {
			if failure.Strategy != "policy" {
				report.Findings = append(report.Findings, conditionalFinding(failure.PlanRecordID, lowered.ID, "required failure "+failure.Code, observed.Evidence))
			}
		}
		return report, nil
	}
	for _, effect := range lowered.Effects {
		if !effect.Required {
			continue
		}
		if foundEffect(observed, effect.Name) {
			report.Findings = append(report.Findings, verifiedFinding(effect.PlanRecordID, lowered.ID, "required effect "+effect.Name, observed.Evidence))
			continue
		}
		report.Findings = append(report.Findings, violatedFinding(effect.PlanRecordID, lowered.ID, "required effect "+effect.Name, observed.Evidence, "Implement "+effect.Name+" with source evidence."))
	}
	for _, failure := range lowered.Failures {
		if failure.Strategy == "policy" {
			continue
		}
		if foundFailure(observed, failure.Code) {
			report.Findings = append(report.Findings, verifiedFinding(failure.PlanRecordID, lowered.ID, "required failure "+failure.Code, observed.Evidence))
			continue
		}
		report.Findings = append(report.Findings, violatedFinding(failure.PlanRecordID, lowered.ID, "required failure "+failure.Code, observed.Evidence, "Propagate or wrap "+failure.Code+" as required by the recipe."))
	}
	for _, step := range lowered.Steps {
		if step.RequiredCall == "" {
			continue
		}
		if foundEffect(observed, step.RequiredCall) {
			report.Findings = append(report.Findings, verifiedFinding(step.PlanRecordID, lowered.ID, "required direct call "+step.RequiredCall, observed.Evidence))
			continue
		}
		report.Findings = append(report.Findings, Finding{PlanRecordID: step.PlanRecordID, RecipeID: lowered.ID, Result: ResultUnsupported, Severity: "info", Expected: "required direct call " + step.RequiredCall, Evidence: observed.Evidence, RepairTarget: "Expose direct-call identity in the language lift or verify the bound call manually."})
		report.Status = statusAfter(report.Status, StatusInconclusive)
	}
	for _, finding := range report.Findings {
		if finding.Result == ResultViolated {
			report.Status = StatusFailed
		}
	}
	return report, nil
}

func statusAfter(current, next Status) Status {
	if current == StatusFailed {
		return current
	}
	return next
}
func foundEffect(observed *lift.Unit, name string) bool {
	for _, effect := range observed.Observed.SideEffects {
		if effect.Name == name {
			return true
		}
	}
	return false
}
func foundFailure(observed *lift.Unit, code string) bool {
	for _, failure := range observed.Observed.FailureModes {
		if failure.Code == code {
			return true
		}
	}
	return false
}
func verifiedFinding(id, recipeID, expected string, evidence []lift.Evidence) Finding {
	return Finding{PlanRecordID: id, RecipeID: recipeID, Result: ResultVerified, Severity: "info", Expected: expected, Observed: expected, Evidence: evidence}
}
func conditionalFinding(id, recipeID, expected string, evidence []lift.Evidence) Finding {
	return Finding{PlanRecordID: id, RecipeID: recipeID, Result: ResultConditional, Severity: "warning", Expected: expected, Evidence: evidence, RepairTarget: "Use a modeled source lift or explicitly waive the requirement."}
}
func unknownFinding(planID, recipeID, expected string, evidence []lift.Evidence) Finding {
	return Finding{PlanRecordID: planID, RecipeID: recipeID, Result: ResultUnknown, Severity: "warning", Expected: expected, Evidence: evidence, RepairTarget: "Provide a source lift before accepting this implementation."}
}
func violatedFinding(id, recipeID, expected string, evidence []lift.Evidence, repair string) Finding {
	return Finding{PlanRecordID: id, RecipeID: recipeID, Result: ResultViolated, Severity: "error", Expected: expected, Evidence: evidence, RepairTarget: repair}
}
