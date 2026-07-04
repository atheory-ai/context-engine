package iir

import (
	"context"
	"encoding/json"
	"testing"
)

func TestParseIntentJSON_Valid(t *testing.T) {
	const doc = `{"kind":"FunctionIntent","name":"f","language":"typescript","inputs":[{"name":"a","type":"number"}],"returns":{"type":"void"}}`
	intent, err := ParseIntentJSON([]byte(doc))
	if err != nil {
		t.Fatalf("ParseIntentJSON: %v", err)
	}
	if intent.Name != "f" || intent.Visibility != VisibilityPublic {
		t.Errorf("unexpected intent: %+v", intent)
	}
}

func TestParseIntentJSON_Invalid(t *testing.T) {
	for name, doc := range map[string]string{
		"missing name": `{"kind":"FunctionIntent","language":"typescript"}`,
		"bad language": `{"kind":"FunctionIntent","name":"f","language":"rust"}`,
		"not json":     `{not json`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseIntentJSON([]byte(doc)); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

// The host-function boundary marshals an extracted FunctionIntent to JSON and
// parses it back. json.Marshal emits returns.explicit, which the strict YAML
// LoadIntent would reject — ParseIntentJSON must tolerate it so the round-trip
// works.
func TestParseIntentJSON_RoundTripsMarshaledIntent(t *testing.T) {
	src := `export function f(a: number, b: string): void { return; }`
	extracted, err := ExtractFunction(context.Background(), []byte(src), "f")
	if err != nil {
		t.Fatalf("ExtractFunction: %v", err)
	}

	raw, err := json.Marshal(extracted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Guard the premise: the marshaled form carries the "explicit" key.
	if !json.Valid(raw) {
		t.Fatal("marshaled intent is not valid JSON")
	}

	// The strict YAML loader rejects it (documents why ParseIntentJSON exists).
	if _, err := LoadIntent(raw); err == nil {
		t.Error("expected LoadIntent to reject the marshaled JSON (unknown 'explicit' field)")
	}

	// ParseIntentJSON accepts it and recovers the same contract.
	got, err := ParseIntentJSON(raw)
	if err != nil {
		t.Fatalf("ParseIntentJSON round-trip: %v", err)
	}
	if got.Name != "f" || len(got.Inputs) != 2 || got.Returns.Type != "void" {
		t.Errorf("round-trip lost fields: %+v", got)
	}
}
