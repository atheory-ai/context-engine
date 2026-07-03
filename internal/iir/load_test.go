package iir

import (
	"strings"
	"testing"
)

const validIntentYAML = `
kind: FunctionIntent
name: validateDonationAmount
language: typescript
inputs:
  - name: amount
    type: Money
returns:
  type: ValidationResult<Money>
sideEffects: []
failureModes:
  - amount_below_minimum
`

func TestLoadIntent_Valid(t *testing.T) {
	intent, err := LoadIntent([]byte(validIntentYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent.Name != "validateDonationAmount" {
		t.Errorf("name = %q, want validateDonationAmount", intent.Name)
	}
	// Visibility defaults to public when omitted.
	if intent.Visibility != VisibilityPublic {
		t.Errorf("visibility = %q, want public", intent.Visibility)
	}
	// A declared return type marks the contract explicit.
	if !intent.Returns.Explicit {
		t.Error("expected Returns.Explicit=true when return type declared")
	}
	if len(intent.Inputs) != 1 || intent.Inputs[0].Type != "Money" {
		t.Errorf("inputs = %+v, want one Money input", intent.Inputs)
	}
}

func TestLoadIntent_JSONInput(t *testing.T) {
	// JSON is a subset of YAML and must load through the same path.
	const j = `{"kind":"FunctionIntent","name":"f","language":"typescript","inputs":[],"returns":{"type":"void"}}`
	intent, err := LoadIntent([]byte(j))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent.Name != "f" {
		t.Errorf("name = %q, want f", intent.Name)
	}
}

func TestLoadIntent_Invalid(t *testing.T) {
	cases := map[string]string{
		"missing kind": `
name: f
language: typescript`,
		"unsupported kind": `
kind: ClassIntent
name: f
language: typescript`,
		"missing name": `
kind: FunctionIntent
language: typescript`,
		"unsupported language": `
kind: FunctionIntent
name: f
language: rust`,
		"unknown field": `
kind: FunctionIntent
name: f
language: typescript
bogusField: true`,
		"empty input name": `
kind: FunctionIntent
name: f
language: typescript
inputs:
  - type: Money`,
		"behavior missing then": `
kind: FunctionIntent
name: f
language: typescript
behavior:
  - when: something happens`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadIntent([]byte(doc)); err == nil {
				t.Errorf("expected error for %s, got nil", name)
			}
		})
	}
}

func TestLoadIntent_ErrorMentionsField(t *testing.T) {
	_, err := LoadIntent([]byte("kind: FunctionIntent\nlanguage: typescript"))
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("expected diagnostic mentioning 'name', got %v", err)
	}
}
