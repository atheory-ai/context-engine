package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	extism "github.com/extism/go-sdk"

	"github.com/atheory-ai/context-engine/internal/iir"
)

// maxIIRPayloadBytes bounds plugin-supplied source/intent strings before they
// reach tree-sitter parsing, so a buggy or hostile plugin can't feed the host
// an unbounded payload on every call. 4 MiB is far above any real function.
const maxIIRPayloadBytes = 4 << 20

// checkPayloadSize returns an error if a plugin-supplied string exceeds the cap.
func checkPayloadSize(name, s string) error {
	if len(s) > maxIIRPayloadBytes {
		return fmt.Errorf("%s exceeds %d bytes", name, maxIIRPayloadBytes)
	}
	return nil
}

// validateExtractLanguage checks the language a plugin asked to extract. Empty
// defaults to the only supported language; anything else is rejected with a
// clear message rather than silently parsed as TypeScript.
func validateExtractLanguage(lang string) error {
	switch lang {
	case "", "typescript":
		return nil
	default:
		return fmt.Errorf("unsupported language %q (only \"typescript\")", lang)
	}
}

// This file exposes the IIR capability to WASM plugins as ce.iir_* host
// functions (RFC docs/specs/iir-specs/11: "IIR is a host capability plugins
// call"). The functions are pure computations over internal/iir — no substrate
// or config needed — so they take no HostDeps.
//
// Errors are returned in-band as a JSON object {"error": "..."} rather than
// trapping the plugin, so a plugin can handle a bad input gracefully. Callers
// distinguish success from failure by checking for the "error" key.

// buildIIRHostFunctions returns the ce.iir_* host functions. Namespaced by the
// caller (buildHostFunctions) alongside the other ce.* functions. The extract
// and verify functions need an extractor (deps.IIRExtractor); generate/gentests
// are pure computations over internal/iir.
func buildIIRHostFunctions(deps HostDeps) []extism.HostFunction {
	return []extism.HostFunction{
		makeHostIIRExtract(deps),
		makeHostIIRVerify(deps),
		makeHostIIRGenerate(),
		makeHostIIRGenTests(),
	}
}

// writeJSON marshals v and writes it to the plugin's memory, setting stack[0].
func writeJSON(p *extism.CurrentPlugin, stack []uint64, v any) {
	out, err := json.Marshal(v)
	if err != nil {
		out = []byte(`{"error":"marshal result"}`)
	}
	offset, _ := p.WriteString(string(out))
	stack[0] = offset
}

// writeErr writes a JSON error object.
func writeErr(p *extism.CurrentPlugin, stack []uint64, msg string) {
	writeJSON(p, stack, map[string]string{"error": msg})
}

// makeHostIIRExtract creates ce.iir_extract(language_ptr, source_ptr, target_ptr)
// → function_intent_json_ptr. Extracts IIR for the named function (or the first
// exported one) from source.
func makeHostIIRExtract(deps HostDeps) extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"iir_extract",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			language, _ := p.ReadString(stack[0])
			source, _ := p.ReadString(stack[1])
			target, _ := p.ReadString(stack[2])
			if err := validateExtractLanguage(language); err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			if err := checkPayloadSize("source", source); err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			if deps.IIRExtractor == nil {
				writeErr(p, stack, "iir extraction is not available in this context")
				return
			}
			res, err := deps.IIRExtractor.Extract(ctx, iir.ExtractionInput{Language: language, Source: []byte(source), Target: target})
			if err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			writeJSON(p, stack, res.Function)
		},
		[]extism.ValueType{extism.ValueTypePTR, extism.ValueTypePTR, extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}

// makeHostIIRVerify creates ce.iir_verify(intent_json_ptr, source_ptr) →
// report_json_ptr. Verifies source against intended IIR using the built-in rule
// pack.
func makeHostIIRVerify(deps HostDeps) extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"iir_verify",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			intentJSON, _ := p.ReadString(stack[0])
			source, _ := p.ReadString(stack[1])
			if err := checkPayloadSize("intent", intentJSON); err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			if err := checkPayloadSize("source", source); err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			intent, err := iir.ParseIntentJSON([]byte(intentJSON))
			if err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			if deps.IIRExtractor == nil {
				writeErr(p, stack, "iir extraction is not available in this context")
				return
			}
			report, err := iir.VerifySource(ctx, deps.IIRExtractor, intent, []byte(source), iir.DefaultRulePack())
			if err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			writeJSON(p, stack, report)
		},
		[]extism.ValueType{extism.ValueTypePTR, extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}

// makeHostIIRGenerate creates ce.iir_generate(intent_json_ptr) → source_ptr.
// Emits TypeScript source from intended IIR. On error, returns a JSON error
// object instead of source.
func makeHostIIRGenerate() extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"iir_generate",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			intentJSON, _ := p.ReadString(stack[0])
			if err := checkPayloadSize("intent", intentJSON); err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			intent, err := iir.ParseIntentJSON([]byte(intentJSON))
			if err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			source, err := iir.GenerateFunction(intent)
			if err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			offset, _ := p.WriteString(source)
			stack[0] = offset
		},
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}

// makeHostIIRGenTests creates ce.iir_gen_tests(intent_json_ptr) →
// test_artifact_json_ptr. Emits tests + coverage from intended IIR.
func makeHostIIRGenTests() extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"iir_gen_tests",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			intentJSON, _ := p.ReadString(stack[0])
			if err := checkPayloadSize("intent", intentJSON); err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			intent, err := iir.ParseIntentJSON([]byte(intentJSON))
			if err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			artifact, err := iir.GenerateTests(intent)
			if err != nil {
				writeErr(p, stack, err.Error())
				return
			}
			writeJSON(p, stack, artifact)
		},
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}
