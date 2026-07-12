package iir

import (
	"context"
	"fmt"
)

// Status is the overall verdict of a verification run.
type Status string

const (
	StatusPassed Status = "passed"
	StatusFailed Status = "failed"
)

// Report is the stable, machine-readable result of verifying source against
// intended IIR. Its shape is a contract for tests and agents — fields are always
// present (never null) so consumers can rely on the structure.
type Report struct {
	Status        Status          `json:"status"`
	Intended      *FunctionIntent `json:"intended"`
	Extracted     *FunctionIntent `json:"extracted"`
	Matches       []Match         `json:"matches"`
	Mismatches    []Mismatch      `json:"mismatches"`
	RuleResults   []RuleResult    `json:"ruleResults"`
	RepairTargets []string        `json:"repairTargets"`
}

// Verify compares intended IIR against extracted IIR, evaluates the rule pack
// against the extracted intent, and assembles a report. Verification fails when
// any mismatch or rule result is at error severity.
func Verify(intended, extracted *FunctionIntent, pack RulePack) *Report {
	// Route through the built-in comparator so the core uses the same plugin
	// interface future comparators will.
	comparison := BuiltinComparator().Compare(intended, extracted)
	ruleResults := EvaluateRules(pack, extracted)

	report := &Report{
		Status:        StatusPassed,
		Intended:      intended,
		Extracted:     extracted,
		Matches:       comparison.Matches,
		Mismatches:    comparison.Mismatches,
		RuleResults:   ruleResults,
		RepairTargets: collectRepairTargets(comparison.Mismatches, ruleResults),
	}

	if hasFailure(comparison.Mismatches, ruleResults) {
		report.Status = StatusFailed
	}
	return report
}

// VerifySource is the end-to-end helper: extract the intended function from
// source with the provided extractor, then verify. The extractor is injected
// (rather than a built-in) so extraction runs through the universal plugin lift
// — the same frontend indexing uses — keeping this package free of the plugin
// runtime.
func VerifySource(ctx context.Context, extractor Extractor, intended *FunctionIntent, source []byte, pack RulePack) (*Report, error) {
	if extractor == nil {
		return nil, fmt.Errorf("no IIR extractor configured")
	}
	input := ExtractionInput{Language: intended.Language, Source: source, Target: intended.Name}
	if !extractor.Supports(input) {
		return nil, fmt.Errorf("no extractor supports language %q", intended.Language)
	}
	result, err := extractor.Extract(ctx, input)
	if err != nil {
		return nil, err
	}
	// Gate the comparator too, symmetric with the extractor check above, so an
	// unsupported pair fails clearly rather than reaching Compare.
	if !BuiltinComparator().Supports(intended, result.Function) {
		return nil, fmt.Errorf("no comparator supports the extracted %s intent", intended.Language)
	}
	return Verify(intended, result.Function, pack), nil
}

func hasFailure(mismatches []Mismatch, ruleResults []RuleResult) bool {
	for _, m := range mismatches {
		if m.Severity == SeverityError {
			return true
		}
	}
	for _, r := range ruleResults {
		if r.Status == RuleFailed {
			return true
		}
	}
	return false
}

// collectRepairTargets aggregates de-duplicated repair guidance in a stable
// order: mismatch repairs first (in comparison order), then rule repairs.
func collectRepairTargets(mismatches []Mismatch, ruleResults []RuleResult) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, m := range mismatches {
		// Info-level findings (e.g. unsupported comparisons) are not actionable
		// repairs, so they don't populate repair targets.
		if m.Severity == SeverityInfo {
			continue
		}
		add(m.RepairTarget)
	}
	for _, r := range ruleResults {
		if r.Status == RuleFailed || r.Status == RuleWarned {
			add(r.Repair)
		}
	}
	return out
}
