package shaper

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
)

// fakeLLM returns canned responses in order, recording each request so tests can
// assert the retry re-prompt carries the prior error.
type fakeLLM struct {
	responses []string
	err       error
	requests  []core.CompletionRequest
}

func (f *fakeLLM) Complete(_ context.Context, req core.CompletionRequest) (core.CompletionResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return core.CompletionResponse{}, f.err
	}
	i := len(f.requests) - 1
	if i >= len(f.responses) {
		i = len(f.responses) - 1
	}
	return core.CompletionResponse{Content: f.responses[i]}, nil
}

func (f *fakeLLM) Stream(context.Context, core.CompletionRequest, chan<- string) error { return nil }
func (f *fakeLLM) ModelInfo() core.ModelInfo                                           { return core.ModelInfo{} }
func (f *fakeLLM) EstimateTokens(string) int                                           { return 0 }

const validIntentResponse = "Here you go:\n```json\n" + `{
  "kind": "FunctionIntent",
  "name": "validateAmount",
  "language": "typescript",
  "inputs": [{"name": "amount", "type": "Money"}],
  "returns": {"type": "ValidationResult<Money>"},
  "sideEffects": []
}` + "\n```\nHope that helps!"

func TestShape_HappyPath(t *testing.T) {
	llm := &fakeLLM{responses: []string{validIntentResponse}}
	intent, err := New(llm).Shape(context.Background(), "validate a donation amount")
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	if intent.Name != "validateAmount" || intent.Language != "typescript" {
		t.Errorf("unexpected intent: %+v", intent)
	}
	// A shaped intent is inferred from a description by a model — not declared.
	if intent.Origin != iir.OriginInferred {
		t.Errorf("origin = %q, want inferred", intent.Origin)
	}
	if len(llm.requests) != 1 {
		t.Errorf("expected 1 model call, got %d", len(llm.requests))
	}
}

func TestShape_RetriesWithErrorFeedback(t *testing.T) {
	// First response is invalid (missing name), second is valid.
	bad := "```json\n{\"kind\":\"FunctionIntent\",\"language\":\"typescript\"}\n```"
	llm := &fakeLLM{responses: []string{bad, validIntentResponse}}

	intent, err := New(llm).Shape(context.Background(), "do a thing")
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	if intent.Name != "validateAmount" {
		t.Errorf("expected recovery on second attempt, got %+v", intent)
	}
	if len(llm.requests) != 2 {
		t.Fatalf("expected 2 model calls, got %d", len(llm.requests))
	}
	// The retry must feed the prior validation error back to the model.
	if !strings.Contains(llm.requests[1].Messages[0].Content, "previous output was rejected") {
		t.Errorf("retry prompt missing error feedback: %q", llm.requests[1].Messages[0].Content)
	}
}

func TestShape_GivesUpAfterMaxAttempts(t *testing.T) {
	llm := &fakeLLM{responses: []string{"no json here", "still no json"}}
	_, err := New(llm).Shape(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error when the model never produces valid IIR")
	}
	if len(llm.requests) != maxAttempts {
		t.Errorf("expected %d attempts, got %d", maxAttempts, len(llm.requests))
	}
}

func TestShape_LLMErrorPropagates(t *testing.T) {
	llm := &fakeLLM{err: errors.New("boom")}
	if _, err := New(llm).Shape(context.Background(), "x"); err == nil {
		t.Error("expected the LLM error to propagate")
	}
}

func TestShape_Guards(t *testing.T) {
	if _, err := New(nil).Shape(context.Background(), "x"); err == nil {
		t.Error("expected error for nil provider")
	}
	if _, err := New(&fakeLLM{responses: []string{validIntentResponse}}).Shape(context.Background(), ""); err == nil {
		t.Error("expected error for empty description")
	}
}
