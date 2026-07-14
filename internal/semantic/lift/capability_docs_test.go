package lift

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCapabilityMatrixDocumentation keeps the public capability guide aligned
// with the v1 host contract. Update this test and the matrix together whenever
// a language gains modeled coverage or a new renderer/API becomes shipped.
func TestCapabilityMatrixDocumentation(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate capability test")
	}
	path := filepath.Join(filepath.Dir(file), "../../../docs/iir-capabilities.md")
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capability matrix: %v", err)
	}
	for _, required := range []string{
		"Indexed TypeScript source lift | shipped",
		"Indexed Go source lift | shipped",
		"Indexed Python source lift | shipped",
		"Plan-aware source lift v1 | experimental",
		"Implementation recipe rendering | shipped",
		"Policy passes | experimental",
		"Cross-language generation / total IL | deferred",
		"make test-iir-golden",
	} {
		if !strings.Contains(string(doc), required) {
			t.Errorf("capability matrix missing %q", required)
		}
	}
	capability := CapabilityFor("typescript")
	if capability.Language != "typescript" || len(capability.SchemaVersions) != 1 || capability.SchemaVersions[0] != SchemaVersionV1 {
		t.Fatalf("unexpected TypeScript capability: %+v", capability)
	}
	if !containsCoverage(capability.Coverage, CoverageModeled) || !containsCoverage(capability.Coverage, CoveragePartial) || !containsCoverage(capability.Coverage, CoverageUnsupported) {
		t.Fatalf("matrix must document all host coverage states: %+v", capability.Coverage)
	}
}

func containsCoverage(values []Coverage, wanted Coverage) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
