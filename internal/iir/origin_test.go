package iir

import "testing"

func TestOrigin_DefaultsToDeclared(t *testing.T) {
	// A hand-authored intent that omits origin is a declaration.
	yamlIntent, err := LoadIntent([]byte("kind: FunctionIntent\nname: f\nlanguage: go\nreturns:\n  type: error\n"))
	if err != nil {
		t.Fatalf("LoadIntent: %v", err)
	}
	if yamlIntent.Origin != OriginDeclared {
		t.Errorf("YAML origin = %q, want declared", yamlIntent.Origin)
	}

	jsonIntent, err := ParseIntentJSON([]byte(`{"kind":"FunctionIntent","name":"f","language":"go","returns":{"type":"error"}}`))
	if err != nil {
		t.Fatalf("ParseIntentJSON: %v", err)
	}
	if jsonIntent.Origin != OriginDeclared {
		t.Errorf("JSON origin = %q, want declared", jsonIntent.Origin)
	}
}

func TestOrigin_PreservesStampedProvenance(t *testing.T) {
	// A plugin lift stamps "observed"; parsing must preserve it, not overwrite it
	// with the declared default.
	intent, err := ParseIntentJSON([]byte(`{"kind":"FunctionIntent","name":"f","language":"go","origin":"observed","returns":{"type":"error"}}`))
	if err != nil {
		t.Fatalf("ParseIntentJSON: %v", err)
	}
	if intent.Origin != OriginObserved {
		t.Errorf("origin = %q, want observed (stamped provenance must survive)", intent.Origin)
	}
}

func TestOrigin_RejectsUnknownValue(t *testing.T) {
	_, err := ParseIntentJSON([]byte(`{"kind":"FunctionIntent","name":"f","language":"go","origin":"guessed","returns":{"type":"error"}}`))
	if err == nil {
		t.Error("expected an error for an unknown origin value")
	}
}
