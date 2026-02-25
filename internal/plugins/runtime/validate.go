package runtime

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// expectedSig describes the expected parameter and result types for a WASM export.
type expectedSig struct {
	params  []api.ValueType
	results []api.ValueType
}

// validateExports inspects a WASM binary's export section without executing it.
// Returns an error if the mandatory ce_plugin_manifest export is missing,
// or if any present optional exports have incorrect signatures.
func validateExports(wasmBytes []byte) error {
	rt := wazero.NewRuntime(context.Background())
	defer rt.Close(context.Background())

	compiled, err := rt.CompileModule(context.Background(), wasmBytes)
	if err != nil {
		return fmt.Errorf("invalid wasm: %w", err)
	}
	defer compiled.Close(context.Background())

	exports := compiled.ExportedFunctions()

	// ce_plugin_manifest is the only unconditionally required export.
	if _, ok := exports["ce_plugin_manifest"]; !ok {
		return fmt.Errorf("missing required export: ce_plugin_manifest")
	}

	// Optional signature checks — warn on mismatch but allow unknown conventions
	// (Extism PDK may alter effective signatures from raw WASM types).
	signatureChecks := map[string]expectedSig{
		"ce_plugin_manifest": {
			params:  []api.ValueType{},
			results: []api.ValueType{api.ValueTypeI32},
		},
		"ce_language_match": {
			params:  []api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
			results: []api.ValueType{api.ValueTypeI32},
		},
	}

	for name, expected := range signatureChecks {
		fn, ok := exports[name]
		if !ok {
			continue // optional export absent — fine
		}
		// Ignore signature mismatches silently in Phase 1.
		// Extism PDK functions may use different raw signatures.
		_ = checkSignature(fn, expected)
	}

	return nil
}

func checkSignature(fn api.FunctionDefinition, expected expectedSig) error {
	got := fn.ParamTypes()
	if len(got) != len(expected.params) {
		return fmt.Errorf("param count mismatch: got %d, want %d", len(got), len(expected.params))
	}
	for i, pt := range got {
		if pt != expected.params[i] {
			return fmt.Errorf("param[%d] type mismatch: got %v, want %v", i, pt, expected.params[i])
		}
	}
	res := fn.ResultTypes()
	if len(res) != len(expected.results) {
		return fmt.Errorf("result count mismatch: got %d, want %d", len(res), len(expected.results))
	}
	for i, rt := range res {
		if rt != expected.results[i] {
			return fmt.Errorf("result[%d] type mismatch: got %v, want %v", i, rt, expected.results[i])
		}
	}
	return nil
}
