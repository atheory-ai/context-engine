package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/runner"
)

// iirExtractor builds a plugin-backed extractor for the verify tool tests,
// skipping when the default plugins aren't built.
func iirExtractor(t *testing.T) iir.Extractor {
	t.Helper()
	dir, err := os.MkdirTemp("", "ce-iir-*")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.DataDir = dir
	ch := core.NewAppChannels()
	ext, _, err := runner.NewIIRExtractor(context.Background(), cfg, &ch)
	if err != nil {
		t.Skipf("iir extractor unavailable: %v", err)
	}
	res, err := ext.Extract(context.Background(), iir.ExtractionInput{
		Language: "typescript", Source: []byte("export function probe(): void {}"), Target: "probe",
	})
	if err != nil || res.Function == nil {
		t.Skip("default plugins not built — run `make bundle-default-plugins`")
	}
	return ext
}

const mcpIntentJSON = `{
	"kind": "FunctionIntent",
	"name": "f",
	"language": "typescript",
	"inputs": [{"name": "x", "type": "number"}],
	"returns": {"type": "number"},
	"sideEffects": []
}`

func TestHandleIIRGenerate(t *testing.T) {
	res, err := handleIIRGenerate()(context.Background(), json.RawMessage(`{"intent": `+mcpIntentJSON+`}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res.Content)
	}
	if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "function f(") {
		t.Errorf("unexpected content: %+v", res.Content)
	}
}

func TestHandleIIRVerify_RoundTrips(t *testing.T) {
	ext := iirExtractor(t)
	gen, _ := handleIIRGenerate()(context.Background(), json.RawMessage(`{"intent": `+mcpIntentJSON+`}`))
	source := gen.Content[0].Text

	args, _ := json.Marshal(map[string]any{
		"intent": json.RawMessage(mcpIntentJSON),
		"source": source,
	})
	res, err := handleIIRVerify(ext, iir.DefaultRulePack)(context.Background(), args)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("verify returned error: %+v", res.Content)
	}
	if !strings.Contains(res.Content[0].Text, `"status": "passed"`) {
		t.Errorf("expected passed report, got: %s", res.Content[0].Text)
	}
}

func TestHandleIIRVerify_UsesProvidedRulePack(t *testing.T) {
	ext := iirExtractor(t)
	// A uniquely-named rule in the provided pack must appear in the report,
	// proving the handler uses the supplied (plugin-merged) pack.
	withPluginRule := func() iir.RulePack {
		return iir.RulePack{Rules: []iir.Rule{{
			ID: "team-plugin-rule", Target: iir.KindFunctionIntent, Severity: iir.SeverityWarning,
		}}}
	}
	args, _ := json.Marshal(map[string]any{
		"intent": json.RawMessage(mcpIntentJSON),
		"source": `export function f(x: number): number { return x; }`,
	})
	res, err := handleIIRVerify(ext, withPluginRule)(context.Background(), args)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("verify returned error: %+v", res.Content)
	}
	if !strings.Contains(res.Content[0].Text, `"team-plugin-rule"`) {
		t.Errorf("report should reflect the provided rule pack, got: %s", res.Content[0].Text)
	}
}

func TestHandleIIRGenTests(t *testing.T) {
	res, err := handleIIRGenTests()(context.Background(), json.RawMessage(`{"intent": `+mcpIntentJSON+`}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError || !strings.Contains(res.Content[0].Text, "describe(") {
		t.Errorf("unexpected result: %+v", res.Content)
	}
}

func TestHandleIIR_InvalidIntentIsErrorResult(t *testing.T) {
	// Invalid intent → an IsError CallToolResult (not a transport error).
	res, err := handleIIRGenerate()(context.Background(), json.RawMessage(`{"intent": {"kind": "FunctionIntent"}}`))
	if err != nil {
		t.Fatalf("should not return a transport error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError result for an invalid intent")
	}
}
