package iir

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadIntentFile reads an intended IIR node from a YAML or JSON file. JSON is a
// subset of YAML, so both formats decode through the same path. It returns
// clear diagnostics for malformed or schema-invalid files.
func LoadIntentFile(path string) (*FunctionIntent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read intent file %s: %w", path, err)
	}
	intent, err := LoadIntent(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return intent, nil
}

// LoadIntent parses intended IIR from raw bytes and validates its schema.
func LoadIntent(data []byte) (*FunctionIntent, error) {
	var intent FunctionIntent
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true) // reject unknown fields so typos surface as errors
	if err := dec.Decode(&intent); err != nil {
		return nil, fmt.Errorf("parse IIR: %w", err)
	}

	normalizeIntent(&intent)
	if err := validateIntent(&intent); err != nil {
		return nil, err
	}
	return &intent, nil
}

// ParseIntentJSON parses intended IIR from JSON and validates its schema. Unlike
// LoadIntent (YAML, strict about unknown fields), this tolerates the extra keys a
// json.Marshal of a FunctionIntent emits (e.g. returns.explicit) so extracted
// IIR round-trips through the JSON host-function boundary.
func ParseIntentJSON(data []byte) (*FunctionIntent, error) {
	var intent FunctionIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return nil, fmt.Errorf("parse IIR JSON: %w", err)
	}
	normalizeIntent(&intent)
	if err := validateIntent(&intent); err != nil {
		return nil, err
	}
	return &intent, nil
}

// normalizeIntent fills in defaults that keep declared IIR concise: intent
// files describe API, so an omitted visibility means public, and a declared
// return type means the contract is explicit.
func normalizeIntent(intent *FunctionIntent) {
	if intent.Visibility == "" {
		intent.Visibility = VisibilityPublic
	}
	intent.Returns.Explicit = intent.Returns.Type != ""
	for i := range intent.Inputs {
		if intent.Inputs[i].Type == "" {
			intent.Inputs[i].Type = TypeUnknown
		}
	}
}

// validateIntent enforces the minimum schema for a FunctionIntent node.
func validateIntent(intent *FunctionIntent) error {
	if intent.Kind == "" {
		return fmt.Errorf("invalid IIR: missing 'kind' (expected %q)", KindFunctionIntent)
	}
	if intent.Kind != KindFunctionIntent {
		return fmt.Errorf("invalid IIR: unsupported kind %q (Slice 1 supports only %q)",
			intent.Kind, KindFunctionIntent)
	}
	if strings.TrimSpace(intent.Name) == "" {
		return fmt.Errorf("invalid IIR: missing 'name'")
	}
	if strings.TrimSpace(intent.Language) == "" {
		return fmt.Errorf("invalid IIR: missing 'language'")
	}
	// Any non-empty language is accepted: under the plugin-owned lift (Track B)
	// each language plugin binds its own constructs into this shared IIR model,
	// so the host does not gate on a fixed language set.
	switch intent.Visibility {
	case VisibilityPublic, VisibilityPrivate:
	default:
		return fmt.Errorf("invalid IIR: unknown visibility %q (expected \"public\" or \"private\")",
			intent.Visibility)
	}
	for i, p := range intent.Inputs {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("invalid IIR: inputs[%d] has empty 'name'", i)
		}
	}
	for i, b := range intent.Behavior {
		if strings.TrimSpace(b.When) == "" || strings.TrimSpace(b.Then) == "" {
			return fmt.Errorf("invalid IIR: behavior[%d] requires both 'when' and 'then'", i)
		}
	}
	return nil
}
