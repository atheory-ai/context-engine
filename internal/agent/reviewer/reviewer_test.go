package reviewer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atheory-ai/context-engine/internal/agent/reviewer"
	"github.com/atheory-ai/context-engine/internal/core"
)

type fakeLLM struct {
	resp core.CompletionResponse
	err  error
}

func (f fakeLLM) Complete(context.Context, core.CompletionRequest) (core.CompletionResponse, error) {
	return f.resp, f.err
}
func (f fakeLLM) Stream(_ context.Context, _ core.CompletionRequest, ch chan<- string) error {
	close(ch)
	return nil
}
func (f fakeLLM) ModelInfo() core.ModelInfo   { return core.ModelInfo{ContextLimit: 200000} }
func (f fakeLLM) EstimateTokens(s string) int { return len(s)/4 + 1 }

func newRC() (*core.RunContext, core.AppChannels) {
	ch := core.NewAppChannels()
	return &core.RunContext{
		Ctx:    context.Background(),
		RunID:  "r",
		TurnID: "t",
		Budget: core.NewBudget(200000),
		Ch:     &ch,
	}, ch
}

func TestRun_ForcedExitConverges(t *testing.T) {
	rc, _ := newRC()
	rc.ForcedExit = true
	// The LLM must not be called on the forced-exit path.
	res, err := reviewer.New(fakeLLM{err: errors.New("should not be called")}, nil).Run(rc, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Converged {
		t.Error("forced exit should report converged")
	}
}

func TestRun_LLMErrorPropagates(t *testing.T) {
	rc, _ := newRC()
	_, err := reviewer.New(fakeLLM{err: errors.New("model down")}, nil).Run(rc, []core.Emission{{Content: "x"}})
	if err == nil {
		t.Error("expected the LLM error to propagate")
	}
}

func TestRun_UnparseableResponseContinuesLoop(t *testing.T) {
	rc, ch := newRC()
	// Non-JSON reviewer output must not fail the run — it continues (non-converged)
	// and warns.
	res, err := reviewer.New(fakeLLM{resp: core.CompletionResponse{Content: "not valid json at all"}}, nil).
		Run(rc, []core.Emission{{Content: "finding"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Converged {
		t.Error("an unparseable response should be treated as non-converged")
	}
	// A warning should have been emitted.
	select {
	case <-ch.Warning:
	default:
		t.Error("expected a parse-error warning emission")
	}
}
