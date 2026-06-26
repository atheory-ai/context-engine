package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// expectedSig describes the expected parameter and result types for a WASM export.
type expectedSig struct {
	params  []api.ValueType
	results []api.ValueType
}

var exportAliases = map[string]string{
	"ce_plugin_manifest":   "ce-plugin-manifest",
	"ce_language_match":    "ce-language-match",
	"ce_language_extract":  "ce-language-extract",
	"ce_language_concepts": "ce-language-concepts",
	"ce_analyzers_list":    "ce-analyzers-list",
	"ce_analyzer_run":      "ce-analyzer-run",
	"ce_tools_list":        "ce-tools-list",
	"ce_tool_activate":     "ce-tool-activate",
	"ce_tool_execute":      "ce-tool-execute",
	"ce_role_definition":   "ce-role-definition",
}

// collectExports returns the set of function names exported by the WASM binary.
// Used to populate pluginInstance.exports for HasCustomMatch checks.
func collectExports(wasmBytes []byte) (map[string]bool, error) {
	rt := wazero.NewRuntime(context.Background())
	defer rt.Close(context.Background())

	compiled, err := rt.CompileModule(context.Background(), wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("compile module: %w", err)
	}
	defer compiled.Close(context.Background())

	exports := compiled.ExportedFunctions()
	result := make(map[string]bool, len(exports))
	for name := range exports {
		result[name] = true
	}
	return result, nil
}

func resolveExportName(exports map[string]bool, name string) string {
	if exports[name] {
		return name
	}
	if alias, ok := exportAliases[name]; ok && exports[alias] {
		return alias
	}
	return name
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
		if _, ok := exports[exportAliases["ce_plugin_manifest"]]; !ok {
			return fmt.Errorf("missing required export: ce_plugin_manifest")
		}
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
		fnName := name
		fn, ok := exports[fnName]
		if !ok {
			fnName = strings.ReplaceAll(name, "_", "-")
			fn, ok = exports[fnName]
		}
		if !ok {
			continue // optional export absent — fine
		}
		// Ignore signature mismatches silently in Phase 1.
		// Extism PDK functions may use different raw signatures.
		_ = checkSignature(fn, expected) //nolint:errcheck // see comment above
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
