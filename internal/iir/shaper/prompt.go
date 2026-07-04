package shaper

import (
	"errors"
	"fmt"
	"strings"
)

// systemPrompt instructs the model to emit a FunctionIntent as JSON. It mirrors
// the iir.FunctionIntent schema; the deterministic ParseIntentJSON is the source
// of truth for validation, so the prompt only needs to steer the model close.
const systemPrompt = `You convert a natural-language description of a function into an Intermediate Intent Representation (IIR) as JSON.

Output ONLY a single JSON object inside a fenced code block:

` + "```json" + `
{
  "kind": "FunctionIntent",
  "name": "<functionName>",
  "language": "typescript",
  "visibility": "public" | "private",
  "inputs": [ { "name": "<param>", "type": "<TsType>" } ],
  "returns": { "type": "<TsType>" },
  "behavior": [ { "when": "<condition>", "then": "<outcome>" } ],
  "sideEffects": [ "<client.method>" ],
  "failureModes": [ "<failure_tag>" ],
  "constraints": [ "<durable expectation>" ]
}
` + "```" + `

Rules:
- kind is always "FunctionIntent"; language is always "typescript".
- Use [] for empty lists. Use "sideEffects": [] to assert no side effects.
- Prefer a Result/ValidationResult return type when the function has failure modes.
- Do not write function bodies or prose outside the JSON block.`

// userPrompt builds the user message. On a retry it appends the prior failure so
// the model can self-correct.
func userPrompt(description string, prevErr error) string {
	if prevErr == nil {
		return description
	}
	return fmt.Sprintf(
		"%s\n\n[Your previous output was rejected: %s. Return a corrected JSON object matching the schema.]",
		description, prevErr.Error(),
	)
}

// extractJSON pulls the JSON object from a model response. It scans each ```
// code fence in order for a JSON object (so a non-JSON fence preceding the real
// one — e.g. a ```text note — is skipped), then falls back to the first balanced
// {...} span anywhere in the response. Returns an error if none is present.
func extractJSON(response string) ([]byte, error) {
	for _, block := range fencedBlocks(response) {
		if obj, ok := firstJSONObject(block); ok {
			return []byte(obj), nil
		}
	}
	if obj, ok := firstJSONObject(response); ok {
		return []byte(obj), nil
	}
	return nil, errors.New("no JSON object found in model response")
}

// fencedBlocks returns the contents of every ``` code fence in order, dropping
// an optional language tag on each opening fence line (e.g. "json").
func fencedBlocks(s string) []string {
	var blocks []string
	for {
		start := strings.Index(s, "```")
		if start < 0 {
			break
		}
		rest := s[start+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		end := strings.Index(rest, "```")
		if end < 0 {
			break
		}
		blocks = append(blocks, rest[:end])
		s = rest[end+3:]
	}
	return blocks
}

// firstJSONObject returns the first brace-balanced {...} span in s, ignoring
// braces inside strings.
func firstJSONObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// ignore structural chars inside strings
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}
