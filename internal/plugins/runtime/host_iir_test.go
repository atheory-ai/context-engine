package runtime

import (
	"strings"
	"testing"
)

func TestValidateExtractLanguage(t *testing.T) {
	for _, ok := range []string{"", "typescript"} {
		if err := validateExtractLanguage(ok); err != nil {
			t.Errorf("validateExtractLanguage(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"go", "python", "rust"} {
		if err := validateExtractLanguage(bad); err == nil {
			t.Errorf("validateExtractLanguage(%q) = nil, want error", bad)
		}
	}
}

func TestCheckPayloadSize(t *testing.T) {
	if err := checkPayloadSize("source", strings.Repeat("a", 1024)); err != nil {
		t.Errorf("small payload rejected: %v", err)
	}
	if err := checkPayloadSize("source", strings.Repeat("a", maxIIRPayloadBytes+1)); err == nil {
		t.Error("oversized payload accepted, want error")
	}
}

func TestBuildHostFunctions_RegistersIIRUnderCeNamespace(t *testing.T) {
	funcs := buildHostFunctions(HostDeps{})

	byName := make(map[string]string, len(funcs)) // name → namespace
	for _, f := range funcs {
		byName[f.Name] = f.Namespace
	}

	for _, want := range []string{"iir_extract", "iir_verify", "iir_generate", "iir_gen_tests"} {
		ns, ok := byName[want]
		if !ok {
			t.Errorf("host function %q not registered", want)
			continue
		}
		if ns != "ce" {
			t.Errorf("host function %q namespace = %q, want ce", want, ns)
		}
	}

	// The IIR functions are additive — the pre-existing ones remain.
	for _, want := range []string{"log", "emit", "substrate_query", "node_id"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("existing host function %q missing after adding IIR", want)
		}
	}
}
