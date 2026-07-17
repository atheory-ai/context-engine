package cli

import (
	"strings"
	"testing"
)

func TestPluginExtract_AcceptsJSONFlag(t *testing.T) {
	cmd := newPluginCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{
		"extract", "fixture.wasm",
		"--input", "missing-input.json",
		"--json",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing input error")
	}
	if !strings.Contains(err.Error(), "read input") {
		t.Fatalf("extract with --json returned %v, want input read error (not an unknown flag)", err)
	}
}

// The JSON here mirrors exactly what the SDK's buildPluginManifest emits for
// iirRules (camelCase keys, forbidConditionShape structural predicate). This is
// the host-side end of the contract validated live via `ce plugin validate`.
func TestPluginIIRRuleIDs_ParsesSDKShape(t *testing.T) {
	raw := []byte(`{"rules":[
		{"id":"com.example/forbid-null-equality","target":"FunctionIntent","severity":"warning",
		 "require":{"forbidConditionShape":{"ops":["==","!=","===","!=="],"operandLiteral":"null"}}}
	]}`)
	ids, err := pluginIIRRuleIDs(raw)
	if err != nil {
		t.Fatalf("valid SDK-shaped pack failed to parse: %v", err)
	}
	if len(ids) != 1 || ids[0] != "com.example/forbid-null-equality" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestPluginIIRRuleIDs_RejectsMalformed(t *testing.T) {
	// Rule missing target/severity — the host is the authoritative validator, so
	// a bad plugin pack must surface as an error (fails `plugin validate`).
	if _, err := pluginIIRRuleIDs([]byte(`{"rules":[{"id":"b"}]}`)); err == nil {
		t.Fatal("expected an error for a rule missing target/severity")
	}
}
