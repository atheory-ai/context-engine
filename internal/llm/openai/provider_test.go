package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

func TestComplete(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"c","object":"chat.completion","created":0,"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test", BaseURL: srv.URL})
	res, err := p.Complete(context.Background(), core.CompletionRequest{
		System:    "be terse",
		Messages:  []core.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.Content != "hi there" {
		t.Errorf("content = %q", res.Content)
	}
	if res.TokensIn != 7 || res.TokensOut != 3 {
		t.Errorf("tokens = %d/%d, want 7/3", res.TokensIn, res.TokensOut)
	}
	if res.FinishReason != "stop" {
		t.Errorf("finish = %q", res.FinishReason)
	}
	// System prompt is prepended as a system message; max tokens uses the modern field.
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v, want 2", body["messages"])
	}
	if first, _ := msgs[0].(map[string]any); first["role"] != "system" {
		t.Errorf("first message role = %v, want system", first["role"])
	}
	if _, ok := body["max_completion_tokens"]; !ok {
		t.Errorf("expected max_completion_tokens in request, got %v", body)
	}
}

func TestStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		chunk := func(delta string) {
			fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", delta)
			if fl != nil {
				fl.Flush()
			}
		}
		chunk("hel")
		chunk("lo")
		fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test", BaseURL: srv.URL})
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

func TestModelInfoAndDefaults(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if info := p.ModelInfo(); info.ID != ModelGPT4o || info.ContextLimit != 128_000 {
		t.Errorf("default model info = %+v", info)
	}
	if info := ModelInfoFor("o1"); info.Tier != core.TierThinking {
		t.Errorf("o1 tier = %q, want thinking", info.Tier)
	}
	if info := ModelInfoFor("unknown-model"); info.Tier != core.TierStandard {
		t.Errorf("unknown model should default to standard, got %q", info.Tier)
	}
	if p.EstimateTokens("abcd") < 1 {
		t.Error("EstimateTokens should be positive")
	}
}
