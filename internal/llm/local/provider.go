// Package local implements core.LLMProvider for a local Ollama server
// (the /api/chat endpoint). No official Go SDK exists, so this is a lean
// net/http client — JSON for completion, newline-delimited JSON for streaming.
package local

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/llm"
)

const (
	defaultBaseURL = "http://localhost:11434"
	defaultModel   = "llama3"
)

// Config holds configuration for the local Ollama provider.
type Config struct {
	BaseURL      string        // Ollama server URL; defaults to http://localhost:11434
	DefaultModel string        // model to use when a request omits one
	HTTPClient   *http.Client  // optional; a 120s-timeout client is used if nil
	Timeout      time.Duration // used only when HTTPClient is nil
}

// Provider implements core.LLMProvider against an Ollama server.
type Provider struct {
	baseURL      string
	defaultModel string
	http         *http.Client
}

// New creates a local Provider from config.
func New(cfg Config) *Provider {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	model := cfg.DefaultModel
	if model == "" {
		model = defaultModel
	}
	hc := cfg.HTTPClient
	if hc == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		hc = &http.Client{Timeout: timeout}
	}
	return &Provider{baseURL: base, defaultModel: model, http: hc}
}

// ── Ollama /api/chat wire types ─────────────────────────────────────────────

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float32 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  chatOptions   `json:"options,omitempty"`
}

type chatResponse struct {
	Message         chatMessage `json:"message"`
	Done            bool        `json:"done"`
	DoneReason      string      `json:"done_reason"`
	PromptEvalCount int         `json:"prompt_eval_count"`
	EvalCount       int         `json:"eval_count"`
}

func (p *Provider) buildRequest(model string, req core.CompletionRequest, stream bool) chatRequest {
	msgs := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		role := m.Role
		if role != "assistant" && role != "system" {
			role = "user"
		}
		msgs = append(msgs, chatMessage{Role: role, Content: m.Content})
	}
	return chatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   stream,
		Options:  chatOptions{Temperature: req.Temperature, NumPredict: req.MaxTokens},
	}
}

func (p *Provider) post(ctx context.Context, body chatRequest) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}
	return resp, nil
}

// Complete sends a non-streaming completion request and returns the full response.
func (p *Provider) Complete(ctx context.Context, req core.CompletionRequest) (core.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	resp, err := p.post(ctx, p.buildRequest(model, req, false))
	if err != nil {
		return core.CompletionResponse{}, fmt.Errorf("local complete: %w", err)
	}
	defer resp.Body.Close()

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return core.CompletionResponse{}, fmt.Errorf("local complete: decode: %w", err)
	}
	return core.CompletionResponse{
		Content:      cr.Message.Content,
		TokensIn:     cr.PromptEvalCount,
		TokensOut:    cr.EvalCount,
		Model:        model,
		FinishReason: cr.DoneReason,
	}, nil
}

// Stream sends a streaming completion request and forwards each content delta to
// ch, closing it when the response completes or the context is cancelled. Ollama
// streams newline-delimited JSON objects, each carrying a partial message.
func (p *Provider) Stream(ctx context.Context, req core.CompletionRequest, ch chan<- string) error {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	resp, err := p.post(ctx, p.buildRequest(model, req, true))
	if err != nil {
		close(ch)
		return fmt.Errorf("local stream: %w", err)
	}
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var chunk chatResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				continue
			}
			if chunk.Message.Content != "" {
				select {
				case ch <- chunk.Message.Content:
				case <-ctx.Done():
					return
				}
			}
			if chunk.Done {
				return
			}
		}
	}()
	return nil
}

// ModelInfo returns metadata about the configured default model. Context limits
// vary by local model and aren't discoverable here, so a conservative default is
// reported.
func (p *Provider) ModelInfo() core.ModelInfo {
	return core.ModelInfo{ID: p.defaultModel, ContextLimit: 32_000, Tier: core.TierFast}
}

// EstimateTokens returns a rough token count using the ~4 chars per token heuristic.
func (p *Provider) EstimateTokens(text string) int { return llm.EstimateTokensRough(text) }
