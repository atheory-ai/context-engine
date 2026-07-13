package iir

import (
	"encoding/json"
	"testing"
)

func TestFailureMode_JSONBackCompat(t *testing.T) {
	// A bare string parses into a code-only failure and round-trips back to a
	// bare string (keeps existing IIR byte-stable).
	var f FailureMode
	if err := json.Unmarshal([]byte(`"amount_below_minimum"`), &f); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if f.Code != "amount_below_minimum" || f.Kind != "" {
		t.Errorf("string form = %+v", f)
	}
	b, _ := json.Marshal(f)
	if string(b) != `"amount_below_minimum"` {
		t.Errorf("plain failure should marshal as a bare string, got %s", b)
	}

	// An object form carries kind/source and marshals as an object.
	if err := json.Unmarshal([]byte(`{"code":"err","kind":"propagated","source":"err"}`), &f); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if f.Code != "err" || f.Kind != FailurePropagated || f.Source != "err" {
		t.Errorf("object form = %+v", f)
	}
	b, _ = json.Marshal(f)
	var round FailureMode
	if err := json.Unmarshal(b, &round); err != nil || round != f {
		t.Errorf("object round-trip changed failure: %+v -> %s -> %+v", f, b, round)
	}
}

func TestFunctionIntent_MixedFailureModeForms(t *testing.T) {
	// A FunctionIntent may mix bare-string and object failure modes on the wire.
	const doc = `{"kind":"FunctionIntent","name":"f","language":"go",
		"returns":{"type":"error","explicit":true},
		"failureModes":["empty id",{"code":"ErrClosed","kind":"sentinel"}]}`
	fi, err := ParseIntentJSON([]byte(doc))
	if err != nil {
		t.Fatalf("ParseIntentJSON: %v", err)
	}
	if len(fi.FailureModes) != 2 {
		t.Fatalf("got %d failure modes, want 2", len(fi.FailureModes))
	}
	if fi.FailureModes[0].Code != "empty id" || fi.FailureModes[0].Kind != "" {
		t.Errorf("failure[0] = %+v", fi.FailureModes[0])
	}
	if fi.FailureModes[1].Code != "ErrClosed" || fi.FailureModes[1].Kind != FailureSentinel {
		t.Errorf("failure[1] = %+v", fi.FailureModes[1])
	}
}

func TestLoadIntent_YAMLFailureModeForms(t *testing.T) {
	// The strict YAML loader accepts bare-string and mapping failure modes.
	const doc = `
kind: FunctionIntent
name: f
language: go
returns:
  type: error
failureModes:
  - empty id
  - code: ErrClosed
    kind: sentinel
`
	fi, err := LoadIntent([]byte(doc))
	if err != nil {
		t.Fatalf("LoadIntent: %v", err)
	}
	if len(fi.FailureModes) != 2 || fi.FailureModes[0].Code != "empty id" ||
		fi.FailureModes[1].Code != "ErrClosed" || fi.FailureModes[1].Kind != FailureSentinel {
		t.Errorf("YAML failure modes = %+v", fi.FailureModes)
	}
}
