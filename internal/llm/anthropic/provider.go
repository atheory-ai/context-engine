package anthropic

import (
	"context"
	"fmt"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/llm"
)

// Config holds configuration for the Anthropic provider.
type Config struct {
	APIKey       string
	BaseURL      string // optional — for proxies or testing
	DefaultModel string // model ID to use when not specified in request
}

// Provider implements core.LLMProvider against the Anthropic Messages API.
type Provider struct {
	client       *anthropicsdk.Client
	defaultModel string
}

// New creates an Anthropic Provider from config.
func New(cfg Config) *Provider {
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	client := anthropicsdk.NewClient(opts...)

	model := cfg.DefaultModel
	if model == "" {
		model = ModelSonnet4
	}

	return &Provider{
		client:       &client,
		defaultModel: model,
	}
}

// Complete sends a completion request to the Anthropic API and returns the full response.
func (p *Provider) Complete(ctx context.Context, req core.CompletionRequest) (core.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	params := buildMessageParams(model, req)

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return core.CompletionResponse{}, fmt.Errorf("anthropic complete: %w", err)
	}

	return extractResponse(model, msg), nil
}

// Stream sends a completion request and streams tokens to the provided channel.
// The channel is closed when the response is complete or an error occurs.
func (p *Provider) Stream(ctx context.Context, req core.CompletionRequest, ch chan<- string) error {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	params := buildMessageParams(model, req)
	stream := p.client.Messages.NewStreaming(ctx, params)

	go func() {
		defer close(ch)
		defer stream.Close()

		for stream.Next() {
			event := stream.Current()
			if event.Type == "content_block_delta" {
				e := event.AsContentBlockDelta()
				if e.Delta.Type == "text_delta" {
					delta := e.Delta.AsTextDelta()
					select {
					case ch <- delta.Text:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return nil
}

// ModelInfo returns metadata about the currently configured default model.
func (p *Provider) ModelInfo() core.ModelInfo {
	return ModelInfoFor(p.defaultModel)
}

// EstimateTokens returns a rough token count using the ~4 chars per token heuristic.
func (p *Provider) EstimateTokens(text string) int {
	return llm.EstimateTokensRough(text)
}

// ============================================================
// Internal helpers
// ============================================================

func buildMessageParams(model string, req core.CompletionRequest) anthropicsdk.MessageNewParams {
	messages := make([]anthropicsdk.MessageParam, len(req.Messages))
	for i, m := range req.Messages {
		switch m.Role {
		case "user":
			messages[i] = anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(m.Content))
		case "assistant":
			messages[i] = anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock(m.Content))
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8096
	}

	params := anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(model),
		Messages:  messages,
		MaxTokens: int64(maxTokens),
	}

	if req.System != "" {
		params.System = []anthropicsdk.TextBlockParam{
			{Text: req.System},
		}
	}

	if req.Thinking != nil && req.Thinking.BudgetTokens > 0 {
		params.Thinking = anthropicsdk.ThinkingConfigParamOfEnabled(int64(req.Thinking.BudgetTokens))
	}

	if req.Temperature > 0 {
		params.Temperature = param.NewOpt(float64(req.Temperature))
	}

	return params
}

func extractResponse(model string, msg *anthropicsdk.Message) core.CompletionResponse {
	var content string
	var thinkingText string

	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropicsdk.TextBlock:
			content += b.Text
		case anthropicsdk.ThinkingBlock:
			thinkingText += b.Thinking
		}
	}

	return core.CompletionResponse{
		Content:      content,
		ThinkingText: thinkingText,
		TokensIn:     int(msg.Usage.InputTokens),
		TokensOut:    int(msg.Usage.OutputTokens),
		Model:        model,
		FinishReason: string(msg.StopReason),
	}
}
