package local

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

func TestComplete(t *testing.T) {
	var gotReq chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(chatResponse{
			Message: chatMessage{Role: "assistant", Content: "hello there"},
			Done:    true, DoneReason: "stop", PromptEvalCount: 5, EvalCount: 2,
		})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, DefaultModel: "llama3"})
	res, err := p.Complete(context.Background(), core.CompletionRequest{
		System:   "be terse",
		Messages: []core.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.Content != "hello there" {
		t.Errorf("content = %q", res.Content)
	}
	if res.TokensIn != 5 || res.TokensOut != 2 {
		t.Errorf("tokens = %d/%d, want 5/2", res.TokensIn, res.TokensOut)
	}
	if res.FinishReason != "stop" || res.Model != "llama3" {
		t.Errorf("finish/model = %q/%q", res.FinishReason, res.Model)
	}
	// The request must be non-streaming with system prompt prepended.
	if gotReq.Stream {
		t.Error("Complete should send stream=false")
	}
	if len(gotReq.Messages) != 2 || gotReq.Messages[0].Role != "system" {
		t.Errorf("messages = %+v, want [system,user]", gotReq.Messages)
	}
}

func TestStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("Stream should send stream=true")
		}
		fl, _ := w.(http.Flusher)
		for _, part := range []string{"hel", "lo"} {
			_ = json.NewEncoder(w).Encode(chatResponse{Message: chatMessage{Content: part}})
			if fl != nil {
				fl.Flush()
			}
		}
		_ = json.NewEncoder(w).Encode(chatResponse{Done: true, DoneReason: "stop"})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	ch := make(chan string, 8)
	if err := p.Stream(context.Background(), core.CompletionRequest{
		Messages: []core.Message{{Role: "user", Content: "hi"}},
	}, ch); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got strings.Builder
	for tok := range ch {
		got.WriteString(tok)
	}
	if got.String() != "hello" {
		t.Errorf("streamed %q, want hello", got.String())
	}
}

func TestStream_Non200ClosesChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	ch := make(chan string, 1)
	if err := p.Stream(context.Background(), core.CompletionRequest{}, ch); err == nil {
		t.Error("expected an error on non-200")
	}
	if _, open := <-ch; open {
		t.Error("channel should be closed on error")
	}
}

func TestDefaultsAndModelInfo(t *testing.T) {
	p := New(Config{})
	if p.baseURL != defaultBaseURL || p.defaultModel != defaultModel {
		t.Errorf("defaults = %q/%q", p.baseURL, p.defaultModel)
	}
	if info := p.ModelInfo(); info.ID != defaultModel || info.Tier != core.TierFast {
		t.Errorf("model info = %+v", info)
	}
	if p.EstimateTokens("abcd") < 1 {
		t.Error("EstimateTokens should be positive")
	}
}
