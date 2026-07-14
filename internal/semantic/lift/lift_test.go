package lift

import (
	"encoding/json"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
)

func observed(t *testing.T) json.RawMessage {
	t.Helper()
	return json.RawMessage(`{"kind":"FunctionIntent","name":"update","language":"typescript","origin":"observed","returns":{"type":"void"},"sideEffects":[],"failureModes":[],"constraints":[]}`)
}

func TestNormalizeModeledLiftRetainsIdentityAndEvidence(t *testing.T) {
	unit, err := Normalize(core.IIRExtracted{NodeID: "plugin-node", SchemaVersion: "v1", Coverage: core.IIRCoverageModeled, Intent: observed(t), Claims: []core.IIRClaim{{ID: "effect", Kind: "effect.db", Statement: "repository.save", Evidence: []core.IIRSourceEvidence{{Path: "src/update.ts", StartByte: 10, EndByte: 30, Basis: "resolved"}}}}, Evidence: []core.IIRSourceEvidence{{Path: "src/update.ts", StartByte: 1, EndByte: 60, Basis: "tree-sitter"}}})
	if err != nil {
		t.Fatal(err)
	}
	if unit.NodeID != "plugin-node" || unit.Observed.Origin != iir.OriginObserved || !unit.CanSatisfyMandatory() || len(unit.Evidence) != 1 || len(unit.Claims) != 1 {
		t.Fatalf("unexpected lift: %+v", unit)
	}
}

func TestNormalizeLegacyPayloadIsExplicitlyPartial(t *testing.T) {
	unit, err := Normalize(core.IIRExtracted{NodeID: "legacy", Intent: observed(t)})
	if err != nil {
		t.Fatal(err)
	}
	if unit.Coverage != CoveragePartial || unit.CanSatisfyMandatory() || len(unit.Claims) != 1 || unit.Claims[0].Kind != "unknown" {
		t.Fatalf("legacy payload must be explicit partial observation: %+v", unit)
	}
}

func TestNormalizeRejectsBadVersionAndUnobservedIntent(t *testing.T) {
	if _, err := Normalize(core.IIRExtracted{NodeID: "bad-version", SchemaVersion: "v2", Coverage: core.IIRCoveragePartial, Intent: observed(t)}); err == nil {
		t.Fatal("expected schema version rejection")
	}
	declared := json.RawMessage(`{"kind":"FunctionIntent","name":"update","language":"typescript","origin":"declared","returns":{"type":"void"},"sideEffects":[],"failureModes":[],"constraints":[]}`)
	if _, err := Normalize(core.IIRExtracted{NodeID: "declared", SchemaVersion: "v1", Coverage: core.IIRCoveragePartial, Intent: declared}); err == nil {
		t.Fatal("expected observed-origin rejection")
	}
}

func TestCapabilityMatrixDeclaresCoverage(t *testing.T) {
	capability := CapabilityFor("typescript")
	if capability.Language != "typescript" || len(capability.SchemaVersions) != 1 || len(capability.Coverage) != 3 {
		t.Fatalf("unexpected capability: %+v", capability)
	}
}
