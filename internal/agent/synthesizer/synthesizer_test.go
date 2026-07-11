package synthesizer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atheory-ai/context-engine/internal/agent/synthesizer"
	"github.com/atheory-ai/context-engine/internal/core"
)

// fakeLLM is a canned core.LLMProvider for driving the synthesizer without a
// real model.
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
func (f fakeLLM) ModelInfo() core.ModelInfo   { return core.ModelInfo{ID: "fake", ContextLimit: 200000} }
func (f fakeLLM) EstimateTokens(s string) int { return len(s)/4 + 1 }

func newRC(emissions ...core.Emission) (*core.RunContext, core.AppChannels) {
	ch := core.NewAppChannels()
	rc := &core.RunContext{
		Ctx:       context.Background(),
		RunID:     "r",
		TurnID:    "t",
		Query:     "what does the code do?",
		Budget:    core.NewBudget(200000),
		Ch:        &ch,
		Emissions: emissions,
	}
	return rc, ch
}

func TestRun_EmitsSynthesizedAnswer(t *testing.T) {
	rc, ch := newRC(core.Emission{Channel: core.ChanThinking, Content: "explored the graph"})
	s := synthesizer.New(fakeLLM{resp: core.CompletionResponse{Content: "the final answer", TokensIn: 10, TokensOut: 5}})

	if err := s.Run(rc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case msg := <-ch.Message:
		if msg.Content != "the final answer" {
			t.Errorf("message = %q, want the final answer", msg.Content)
		}
	default:
		t.Fatal("no message emitted")
	}
}

func TestRun_FallsBackOnLLMError(t *testing.T) {
	rc, ch := newRC(core.Emission{Channel: core.ChanThinking, Content: "partial reasoning"})
	s := synthesizer.New(fakeLLM{err: errors.New("model down")})

	if err := s.Run(rc); err != nil {
		t.Fatalf("Run should recover from an LLM error, got %v", err)
	}
	// The fallback still emits a message rather than failing the run.
	select {
	case msg := <-ch.Message:
		if msg.Content == "" {
			t.Error("fallback message should be non-empty")
		}
	default:
		t.Fatal("no fallback message emitted")
	}
}

func TestRun_ForcedExitProducesPartial(t *testing.T) {
	rc, ch := newRC(core.Emission{Channel: core.ChanThinking, Content: "some findings"})
	rc.ForcedExit = true
	rc.ForcedExitReason = "budget exhausted"
	s := synthesizer.New(fakeLLM{resp: core.CompletionResponse{Content: "partial answer"}})

	if err := s.Run(rc); err != nil {
		t.Fatalf("Run (forced exit): %v", err)
	}
	select {
	case <-ch.Message:
	default:
		t.Fatal("forced-exit synthesis should still emit a message")
	}
}
