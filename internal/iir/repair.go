package iir

import "context"

// This file begins Phase 6 (harness integration): the iterative repair loop that
// drives "shape intent → verify → repair → verify" toward verified code.
//
// The loop is deterministic. The place a model plugs in — proposing a repair
// from a verification report — is an interface (RepairStage), per the spec's
// guidance to keep model-facing stages as interfaces only. The built-in stage
// is deterministic so the loop is fully testable without a model.

// RepairContext is what a repair stage receives: the intended IIR, the current
// source, and the latest verification report (with its repair targets).
type RepairContext struct {
	Intent *FunctionIntent
	Source string
	Report *Report
}

// RepairStage proposes a new source that better matches the intent. This is the
// model-facing extension point: a model-backed stage would make targeted edits
// from the report's repair targets, preserving human-written logic. Built-in
// stages are deterministic.
type RepairStage interface {
	ID() string
	Propose(ctx context.Context, rc RepairContext) (string, error)
}

// RegenerateStage is the built-in deterministic repair strategy: regenerate the
// source from the intent. Because generated code round-trips, this converges in
// one step for the supported subset. It is the baseline — a model-backed stage
// edits the existing source instead of replacing it.
type RegenerateStage struct{}

func (RegenerateStage) ID() string { return "builtin.regenerate" }

func (RegenerateStage) Propose(_ context.Context, rc RepairContext) (string, error) {
	return GenerateFunction(rc.Intent)
}

// Compile-time proof the built-in stage satisfies the interface.
var _ RepairStage = RegenerateStage{}

// ApprovalFunc gates whether a proposed repair is applied — the user-approval
// point. A nil ApprovalFunc auto-approves every proposal.
type ApprovalFunc func(iteration int, proposed string, rc RepairContext) bool

// RepairOptions configures the loop.
type RepairOptions struct {
	MaxIterations int          // maximum repair attempts; defaults to 3 when <= 0
	Approve       ApprovalFunc // nil = auto-approve
}

// RepairIteration records one pass: the source that was verified, its report,
// and (when the report failed) the proposed repair and whether it was applied.
type RepairIteration struct {
	Source   string
	Report   *Report
	Proposed string
	Applied  bool
}

// RepairResult is the outcome of the loop.
type RepairResult struct {
	FinalSource string
	FinalReport *Report
	Converged   bool
	Iterations  []RepairIteration
}

// RepairLoop verifies source against intent and, while verification fails, asks
// the stage to propose a repair (gated by approval), applies it, and re-verifies
// — up to MaxIterations attempts. It converges when a verification passes, and
// stops (unconverged) on max attempts or a rejected proposal. Deterministic
// given a deterministic stage and approval.
func RepairLoop(
	ctx context.Context,
	intent *FunctionIntent,
	source string,
	pack RulePack,
	stage RepairStage,
	opts RepairOptions,
) (*RepairResult, error) {
	maxAttempts := opts.MaxIterations
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	result := &RepairResult{}
	current := source

	for attempt := 0; ; attempt++ {
		report, err := VerifySource(ctx, intent, []byte(current), pack)
		if err != nil {
			return nil, err
		}
		iter := RepairIteration{Source: current, Report: report}

		if report.Status == StatusPassed {
			result.Iterations = append(result.Iterations, iter)
			return finish(result, current, report, true), nil
		}
		if attempt >= maxAttempts {
			result.Iterations = append(result.Iterations, iter)
			return finish(result, current, report, false), nil
		}

		rc := RepairContext{Intent: intent, Source: current, Report: report}
		proposed, err := stage.Propose(ctx, rc)
		if err != nil {
			return nil, err
		}
		iter.Proposed = proposed
		iter.Applied = opts.Approve == nil || opts.Approve(attempt, proposed, rc)
		result.Iterations = append(result.Iterations, iter)

		if !iter.Applied {
			// The proposal was rejected: stop with the current failing state.
			return finish(result, current, report, false), nil
		}
		current = proposed
	}
}

func finish(result *RepairResult, source string, report *Report, converged bool) *RepairResult {
	result.FinalSource = source
	result.FinalReport = report
	result.Converged = converged
	return result
}
