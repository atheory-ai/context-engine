package openai

import (
	"context"
	"fmt"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/llm"
)

// Config holds configuration for the OpenAI provider.
type Config struct {
	APIKey       string
	BaseURL      string // optional — for Azure/OpenAI-compatible endpoints or testing
	DefaultModel string // model ID to use when a request omits one
	MaxRetries   int    // 0 uses the SDK default
	// ReasoningEffort is sent as OpenAI's reasoning_effort request field when
	// non-empty. OpenAI validates model-specific support at request time.
	ReasoningEffort string
}

// Provider implements core.LLMProvider against the OpenAI Chat Completions API.
type Provider struct {
	client          openai.Client
	defaultModel    string
	reasoningEffort shared.ReasoningEffort
}

// New creates an OpenAI Provider from config.
func New(cfg Config) *Provider {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.MaxRetries > 0 {
		opts = append(opts, option.WithMaxRetries(cfg.MaxRetries))
	}
	model := cfg.DefaultModel
	if model == "" {
		model = ModelGPT4o
	}
	return &Provider{
		client:          openai.NewClient(opts...),
		defaultModel:    model,
		reasoningEffort: shared.ReasoningEffort(cfg.ReasoningEffort),
	}
}

// Complete sends a completion request and returns the full response. The SDK
// retries transient errors internally.
func (p *Provider) Complete(ctx context.Context, req core.CompletionRequest) (core.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	res, err := p.client.Chat.Completions.New(ctx, buildParams(model, req, p.reasoningEffort))
	if err != nil {
		return core.CompletionResponse{}, fmt.Errorf("openai complete: %w", err)
	}
	var content, finish string
	if len(res.Choices) > 0 {
		content = res.Choices[0].Message.Content
		finish = res.Choices[0].FinishReason
	}
	return core.CompletionResponse{
		Content:      content,
		TokensIn:     int(res.Usage.PromptTokens),
		TokensOut:    int(res.Usage.CompletionTokens),
		Model:        model,
		FinishReason: finish,
	}, nil
}

// Stream sends a completion request and streams text deltas to ch, closing it
// when the response completes or the context is cancelled.
func (p *Provider) Stream(ctx context.Context, req core.CompletionRequest, ch chan<- string) error {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	stream := p.client.Chat.Completions.NewStreaming(ctx, buildParams(model, req, p.reasoningEffort))
	go func() {
		defer close(ch)
		defer stream.Close()
		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta.Content
			if delta == "" {
				continue
			}
			select {
			case ch <- delta:
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

// ModelInfo returns metadata about the configured default model.
func (p *Provider) ModelInfo() core.ModelInfo { return ModelInfoFor(p.defaultModel) }

// EstimateTokens returns a rough token count using the ~4 chars per token heuristic.
func (p *Provider) EstimateTokens(text string) int { return llm.EstimateTokensRough(text) }

// buildParams maps a core.CompletionRequest onto OpenAI Chat Completions params.
// A system prompt becomes a leading system message; max tokens uses the modern
// max_completion_tokens field (required by the o-series, accepted by gpt-4o).
func buildParams(model string, req core.CompletionRequest, reasoningEffort shared.ReasoningEffort) openai.ChatCompletionNewParams {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.SystemMessage(req.System))
	}
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			msgs = append(msgs, openai.AssistantMessage(m.Content))
			continue
		}
		msgs = append(msgs, openai.UserMessage(m.Content))
	}
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(model),
		Messages: msgs,
	}
	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = param.NewOpt(int64(req.MaxTokens))
	}
	if req.Temperature > 0 {
		params.Temperature = param.NewOpt(float64(req.Temperature))
	}
	if reasoningEffort != "" {
		params.ReasoningEffort = reasoningEffort
	}
	return params
}
