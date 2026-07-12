package iir

import (
	"encoding/json"
	"testing"
)

func TestSideEffect_JSONBackCompat(t *testing.T) {
	// A bare string parses into a name-only effect and round-trips back to a
	// bare string (keeps existing IIR byte-stable).
	var e SideEffect
	if err := json.Unmarshal([]byte(`"analytics.track"`), &e); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if e.Name != "analytics.track" || e.Kind != "" {
		t.Errorf("string form = %+v", e)
	}
	b, _ := json.Marshal(e)
	if string(b) != `"analytics.track"` {
		t.Errorf("plain effect should marshal as a bare string, got %s", b)
	}

	// An object form carries kind/confidence and marshals as an object.
	if err := json.Unmarshal([]byte(`{"name":"db.save","kind":"db","confidence":"high"}`), &e); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if e.Name != "db.save" || e.Kind != "db" || e.Confidence != "high" {
		t.Errorf("object form = %+v", e)
	}
	b, _ = json.Marshal(e)
	var round SideEffect
	if err := json.Unmarshal(b, &round); err != nil || round != e {
		t.Errorf("object round-trip changed effect: %+v -> %s -> %+v", e, b, round)
	}
}

func TestFunctionIntent_MixedSideEffectForms(t *testing.T) {
	// A FunctionIntent may mix bare-string and object effects on the wire.
	const doc = `{"kind":"FunctionIntent","name":"f","language":"go",
		"returns":{"type":"","explicit":false},
		"sideEffects":["analytics.track",{"name":"db.save","kind":"db"}]}`
	fi, err := ParseIntentJSON([]byte(doc))
	if err != nil {
		t.Fatalf("ParseIntentJSON: %v", err)
	}
	if len(fi.SideEffects) != 2 {
		t.Fatalf("got %d effects, want 2", len(fi.SideEffects))
	}
	if fi.SideEffects[0].Name != "analytics.track" || fi.SideEffects[0].Kind != "" {
		t.Errorf("effect[0] = %+v", fi.SideEffects[0])
	}
	if fi.SideEffects[1].Name != "db.save" || fi.SideEffects[1].Kind != "db" {
		t.Errorf("effect[1] = %+v", fi.SideEffects[1])
	}
}

func TestLoadIntent_YAMLSideEffectForms(t *testing.T) {
	// The strict YAML loader accepts bare-string and mapping effects.
	const doc = `
kind: FunctionIntent
name: f
language: go
returns:
  type: error
sideEffects:
  - analytics.track
  - name: db.save
    kind: db
`
	fi, err := LoadIntent([]byte(doc))
	if err != nil {
		t.Fatalf("LoadIntent: %v", err)
	}
	if len(fi.SideEffects) != 2 || fi.SideEffects[0].Name != "analytics.track" ||
		fi.SideEffects[1].Name != "db.save" || fi.SideEffects[1].Kind != "db" {
		t.Errorf("YAML effects = %+v", fi.SideEffects)
	}
}

func TestClassifyEffect(t *testing.T) {
	cases := []struct {
		name, kind, conf string
	}{
		{"http.Get", EffectNetwork, ConfidenceHigh},
		{"db.Save", EffectDB, ConfidenceHigh},
		{"os.WriteFile", EffectIO, ConfidenceHigh},
		{"fmt.Println", EffectLog, ConfidenceHigh},
		{"analytics.track", EffectMutation, ConfidenceHigh},
		{"helper.compute", EffectUnclassified, ConfidenceLow},
	}
	for _, c := range cases {
		k, conf := ClassifyEffect(c.name)
		if k != c.kind || conf != c.conf {
			t.Errorf("ClassifyEffect(%q) = (%q,%q), want (%q,%q)", c.name, k, conf, c.kind, c.conf)
		}
	}
}

// A recognized undeclared effect is an error; an unrecognized (low-confidence)
// one is only a warning, so an over-eager heuristic detection doesn't fail verify.
func TestCompareSideEffects_GradedByConfidence(t *testing.T) {
	base := baseIntent()

	highExtracted := baseIntent()
	highExtracted.SideEffects = stringEffects("http.Get")
	_, mismatches := Compare(base, highExtracted)
	if m := findMismatch(mismatches, MismatchUndeclaredEffect); m == nil || m.Severity != SeverityError {
		t.Errorf("recognized undeclared effect should be an error, got %+v", mismatches)
	}

	lowExtracted := baseIntent()
	lowExtracted.SideEffects = stringEffects("helper.compute")
	_, mismatches = Compare(base, lowExtracted)
	m := findMismatch(mismatches, MismatchUndeclaredEffect)
	if m == nil || m.Severity != SeverityWarning {
		t.Errorf("unrecognized undeclared effect should be a warning, got %+v", mismatches)
	}
}
