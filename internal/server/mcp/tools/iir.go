package tools

import (
	"context"
	"encoding/json"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/server/mcp/protocol"
)

// IIR tools expose the Intermediate Intent Representation over MCP — the RFC's
// "intent → code on every surface". They are pure computations over
// internal/iir and don't use the engine.

var iirVerifyTool = protocol.Tool{
	Name: "ce_iir_verify",
	Description: `Verify TypeScript source against declared intent (IIR).
Returns a verification report: matches, mismatches (with repair targets), and rule results.
Use to check whether code expresses the intended contract.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"intent": {"type": "object", "description": "The intended FunctionIntent (IIR)."},
			"source": {"type": "string", "description": "TypeScript source to verify."}
		},
		"required": ["intent", "source"]
	}`),
}

var iirGenerateTool = protocol.Tool{
	Name: "ce_iir_generate",
	Description: `Generate deterministic TypeScript source from declared intent (IIR).
Returns a source skeleton whose structure matches the intent.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"intent": {"type": "object", "description": "The intended FunctionIntent (IIR)."}
		},
		"required": ["intent"]
	}`),
}

var iirGenTestsTool = protocol.Tool{
	Name: "ce_iir_gen_tests",
	Description: `Generate tests from declared intent (IIR).
Returns test source plus a coverage report over the IIR's behaviors, failure modes, and side effects.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"intent": {"type": "object", "description": "The intended FunctionIntent (IIR)."}
		},
		"required": ["intent"]
	}`),
}

// RegisterIIR registers the ce_iir_* MCP tools. IIR needs no engine, so the
// Registrar's Engine() is unused here.
func RegisterIIR(s Registrar) {
	s.RegisterTool(iirVerifyTool, handleIIRVerify())
	s.RegisterTool(iirGenerateTool, handleIIRGenerate())
	s.RegisterTool(iirGenTestsTool, handleIIRGenTests())
}

func handleIIRVerify() HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		var params struct {
			Intent json.RawMessage `json:"intent"`
			Source string          `json:"source"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.CallToolResult{}, err
		}
		intent, err := iir.ParseIntentJSON(params.Intent)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		report, err := iir.VerifySource(ctx, intent, []byte(params.Source), iir.DefaultRulePack())
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return jsonResult(report)
	}
}

func handleIIRGenerate() HandlerFunc {
	return func(_ context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		intent, errResult, ok := parseIntentArg(args)
		if !ok {
			return errResult, nil
		}
		source, err := iir.GenerateFunction(intent)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return textResult(source), nil
	}
}

func handleIIRGenTests() HandlerFunc {
	return func(_ context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		intent, errResult, ok := parseIntentArg(args)
		if !ok {
			return errResult, nil
		}
		artifact, err := iir.GenerateTests(intent)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return jsonResult(artifact)
	}
}

// parseIntentArg unmarshals an {"intent": {...}} argument. On failure it returns
// an error CallToolResult and ok=false.
func parseIntentArg(args json.RawMessage) (*iir.FunctionIntent, protocol.CallToolResult, bool) {
	var params struct {
		Intent json.RawMessage `json:"intent"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, errorResult(err.Error()), false
	}
	intent, err := iir.ParseIntentJSON(params.Intent)
	if err != nil {
		return nil, errorResult(err.Error()), false
	}
	return intent, protocol.CallToolResult{}, true
}

// jsonResult marshals v to indented JSON as a single text result.
func jsonResult(v any) (protocol.CallToolResult, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return protocol.CallToolResult{}, err
	}
	return textResult(string(out)), nil
}
