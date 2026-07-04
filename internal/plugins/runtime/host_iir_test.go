package runtime

import "testing"

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
