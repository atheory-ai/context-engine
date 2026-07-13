package iir

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// Failure kinds classify how a function signals a failure — the identity axis
// that tells an LLM/verifier not just *that* a function can fail but *how*.
const (
	// FailureConstructed: the function creates the error inline —
	// errors.New/fmt.Errorf("…"), panic("…"), throw new Error("…"),
	// raise ValueError("…"). The Code is the message.
	FailureConstructed = "constructed"
	// FailureSentinel: a named, reusable error value or type — a Go Err* sentinel,
	// a thrown/raised custom error class with no message. The Code is the symbol.
	FailureSentinel = "sentinel"
	// FailurePropagated: an upstream failure forwarded on — Go `return nil, err`,
	// a bare `throw`/`raise` re-raise, or `throw err`. The function does not own
	// this failure; it passes one through. Source names what is forwarded (the
	// variable), or is empty for an anonymous re-raise.
	FailurePropagated = "propagated"
)

// FailureMode is an expected failure outcome. On the wire it is either a bare
// string (the code, e.g. "amount_below_minimum") or an object carrying an
// optional kind and source — both forms parse, and a code-only failure marshals
// back to a bare string so existing IIR stays byte-stable.
type FailureMode struct {
	Code string `json:"code" yaml:"code"`
	Kind string `json:"kind,omitempty" yaml:"kind,omitempty"`
	// Source is set for a propagated failure: the forwarded identifier (or empty
	// for an anonymous re-raise). It is meaningless for constructed/sentinel.
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
}

// plain reports whether only the code is set (no kind/source), so the failure
// round-trips as a bare string.
func (f FailureMode) plain() bool { return f.Kind == "" && f.Source == "" }

func (f FailureMode) MarshalJSON() ([]byte, error) {
	if f.plain() {
		return json.Marshal(f.Code)
	}
	type alias FailureMode
	return json.Marshal(alias(f))
}

// UnmarshalJSON accepts a bare string or an object.
func (f *FailureMode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = FailureMode{Code: s}
		return nil
	}
	type alias FailureMode
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*f = FailureMode(a)
	return nil
}

// UnmarshalYAML accepts a scalar string or a mapping.
func (f *FailureMode) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*f = FailureMode{Code: value.Value}
		return nil
	}
	type alias FailureMode
	var a alias
	if err := value.Decode(&a); err != nil {
		return err
	}
	*f = FailureMode(a)
	return nil
}

// failureCodes returns just the codes, for set operations and messages.
func failureCodes(modes []FailureMode) []string {
	out := make([]string, len(modes))
	for i, m := range modes {
		out[i] = m.Code
	}
	return out
}

// stringFailures builds FailureModes from bare codes (the common case: a plugin
// or author supplied only names).
func stringFailures(codes ...string) []FailureMode {
	out := make([]FailureMode, len(codes))
	for i, c := range codes {
		out[i] = FailureMode{Code: c}
	}
	return out
}
