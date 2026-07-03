package iir

import "context"

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
	matches, mismatches := Compare(intended, extracted)
	ruleResults := EvaluateRules(pack, extracted)

	report := &Report{
		Status:        StatusPassed,
		Intended:      intended,
		Extracted:     extracted,
		Matches:       matches,
		Mismatches:    mismatches,
		RuleResults:   ruleResults,
		RepairTargets: collectRepairTargets(mismatches, ruleResults),
	}

	if hasFailure(mismatches, ruleResults) {
		report.Status = StatusFailed
	}
	return report
}

// VerifySource is the end-to-end helper: extract the intended function from
// source, then verify. It is the path the CLI uses.
func VerifySource(ctx context.Context, intended *FunctionIntent, source []byte, pack RulePack) (*Report, error) {
	extracted, err := ExtractFunction(ctx, source, intended.Name)
	if err != nil {
		return nil, err
	}
	return Verify(intended, extracted, pack), nil
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
