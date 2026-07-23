// Package shaper turns a natural-language description into a validated IIR
// FunctionIntent using an LLM. It is the one model-facing piece of the IIR
// feature; internal/iir itself stays deterministic and model-free.
//
// The model produces a candidate intent; it is never trusted raw. Every
// response is run through iir.ParseIntentJSON (the same deterministic validation
// hand-authored IIR gets), and a bad response is fed back to the model and
// retried — the pattern the Strategizer uses for its IR.
package shaper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/vocabulary"
)

// maxAttempts is the number of model calls before giving up. The second attempt
// re-prompts with the previous validation error so the model can self-correct.
const maxAttempts = 2

// maxTokens caps the shaping response. A single FunctionIntent is small.
const maxTokens = 2048

// Shaper turns natural language into a FunctionIntent via an LLM.
type Shaper struct {
	llm core.LLMProvider
}

// Candidate keeps model uncertainty out of FunctionIntent. The intent contains
// only facts the model is prepared to state; OpenQuestions carry missing product
// decisions forward to semantic resolution instead of encoding them as empty
// arrays or invented conventions.
type Candidate struct {
	Intent        *iir.FunctionIntent
	OpenQuestions []OpenQuestion
	// SemanticTags are controlled context facts proposed by the model. They are
	// carried separately from FunctionIntent so observed source IIR remains
	// compatible; semantic planning records them as inferred claims.
	SemanticTags []string
}

type OpenQuestion struct {
	Field    string `json:"field"`
	Prompt   string `json:"prompt"`
	Blocking bool   `json:"blocking"`
}

// New creates a Shaper backed by the given model provider.
func New(llm core.LLMProvider) *Shaper {
	return &Shaper{llm: llm}
}

// Shape converts a natural-language description into a validated FunctionIntent.
// It retries up to maxAttempts, feeding a parse/validation error back to the
// model between attempts. Returns an error if the model never produces valid
// IIR.
func (s *Shaper) Shape(ctx context.Context, description string) (*iir.FunctionIntent, error) {
	return s.ShapeForLanguage(ctx, description, "typescript")
}

// ShapeForLanguage shapes a candidate for the requested target language.
// Existing Shape callers retain the historical TypeScript default.
func (s *Shaper) ShapeForLanguage(ctx context.Context, description, language string) (*iir.FunctionIntent, error) {
	candidate, err := s.ShapeCandidateForLanguage(ctx, description, language)
	if err != nil {
		return nil, err
	}
	return candidate.Intent, nil
}

// ShapeCandidate produces the validated intent plus explicit unresolved
// decisions. The legacy Shape method remains for callers that only need an
// intent, while semantic planning uses the richer candidate boundary.
func (s *Shaper) ShapeCandidate(ctx context.Context, description string) (*Candidate, error) {
	return s.ShapeCandidateForLanguage(ctx, description, "typescript")
}

// ShapeCandidateForLanguage is the language-aware structured shaping boundary.
func (s *Shaper) ShapeCandidateForLanguage(ctx context.Context, description, language string) (*Candidate, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("shaper: no LLM provider configured")
	}
	if description == "" {
		return nil, fmt.Errorf("shaper: empty description")
	}
	language = strings.TrimSpace(language)
	if language == "" {
		language = "typescript"
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := s.llm.Complete(ctx, core.CompletionRequest{
			System:    systemPromptForLanguage(language),
			Messages:  []core.Message{{Role: "user", Content: userPrompt(description, lastErr)}},
			MaxTokens: maxTokens,
		})
		if err != nil {
			return nil, fmt.Errorf("shaper LLM: %w", err)
		}

		raw, err := extractJSON(resp.Content)
		if err != nil {
			lastErr = err
			continue
		}
		candidate, err := parseCandidate(raw)
		if err != nil {
			lastErr = err
			continue
		}
		// The shaper infers this intent from a natural-language description via a
		// model — mark it inferred so downstream tools know to confirm it.
		candidate.Intent.Origin = iir.OriginInferred
		return candidate, nil
	}

	return nil, fmt.Errorf("shaper: model did not produce valid IIR after %d attempts: %w", maxAttempts, lastErr)
}

func parseCandidate(raw []byte) (*Candidate, error) {
	var envelope struct {
		Intent        json.RawMessage `json:"intent"`
		OpenQuestions []OpenQuestion  `json:"openQuestions"`
		SemanticTags  []string        `json:"semanticTags"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse candidate envelope: %w", err)
	}
	intentRaw := raw
	if len(envelope.Intent) > 0 {
		intentRaw = envelope.Intent
	}
	intent, err := iir.ParseIntentJSON(intentRaw)
	if err != nil {
		return nil, err
	}
	for index, question := range envelope.OpenQuestions {
		if strings.TrimSpace(question.Prompt) == "" {
			return nil, fmt.Errorf("candidate openQuestions[%d] requires prompt", index)
		}
	}
	tags, err := vocabulary.Normalize(envelope.SemanticTags)
	if err != nil {
		return nil, fmt.Errorf("candidate semanticTags: %w", err)
	}
	return &Candidate{Intent: intent, OpenQuestions: envelope.OpenQuestions, SemanticTags: tags}, nil
}
