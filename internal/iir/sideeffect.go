package iir

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// Effect kinds classify an observable side effect. "unclassified" is used when a
// detected effect doesn't match any known category.
const (
	EffectNetwork      = "network"
	EffectDB           = "db"
	EffectIO           = "io"
	EffectLog          = "log"
	EffectMutation     = "mutation"
	EffectUnclassified = "unclassified"
)

// How an effect's kind was established. "resolved" means it matched a known
// effectful API (an import path or recognized client) — deterministic knowledge,
// not a probabilistic guess. "heuristic" means it was inferred from a method-name
// verb or is uncategorized. The comparator grades an undeclared resolved effect
// as an error and a heuristic one as a warning: it should not fail verification
// on a guess.
const (
	BasisResolved  = "resolved"
	BasisHeuristic = "heuristic"
)

// Effect confidence levels, retained for back-compat with IIR that graded
// effects by confidence before basis existed. "high" maps to resolved, "low" to
// heuristic.
const (
	ConfidenceHigh = "high"
	ConfidenceLow  = "low"
)

// SideEffect is an observable effect a function performs. On the wire it is
// either a bare string (the effect name, e.g. "analytics.track") or an object
// carrying an optional kind and basis — both forms parse, and a name-only effect
// marshals back to a bare string so existing IIR stays byte-stable.
type SideEffect struct {
	Name  string `json:"name" yaml:"name"`
	Kind  string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Basis string `json:"basis,omitempty" yaml:"basis,omitempty"`
	// Confidence is kept for back-compat with older effect objects and for the
	// inferred-intent layer; new plugins emit basis instead.
	Confidence string `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// plain reports whether only the name is set, so the effect round-trips as a
// bare string.
func (e SideEffect) plain() bool { return e.Kind == "" && e.Basis == "" && e.Confidence == "" }

func (e SideEffect) MarshalJSON() ([]byte, error) {
	if e.plain() {
		return json.Marshal(e.Name)
	}
	type alias SideEffect
	return json.Marshal(alias(e))
}

// UnmarshalJSON accepts a bare string or an object.
func (e *SideEffect) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*e = SideEffect{Name: s}
		return nil
	}
	type alias SideEffect
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*e = SideEffect(a)
	return nil
}

// UnmarshalYAML accepts a scalar string or a mapping.
func (e *SideEffect) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*e = SideEffect{Name: value.Value}
		return nil
	}
	type alias SideEffect
	var a alias
	if err := value.Decode(&a); err != nil {
		return err
	}
	*e = SideEffect(a)
	return nil
}

// effectNames returns just the names, for set operations and messages.
func effectNames(effects []SideEffect) []string {
	out := make([]string, len(effects))
	for i, e := range effects {
		out[i] = e.Name
	}
	return out
}

// stringEffects builds SideEffects from bare names (the common case: a plugin or
// author supplied only names).
func stringEffects(names ...string) []SideEffect {
	out := make([]SideEffect, len(names))
	for i, n := range names {
		out[i] = SideEffect{Name: n}
	}
	return out
}

// effectCategories maps a curated substring to a kind. An effect name matching
// one is a recognized (resolved) effectful API. This host classifier is the
// fallback for name-only effects (older IIR, hand-authored intents); plugins
// classify structurally at extraction and carry the basis themselves.
var effectCategories = []struct {
	kind     string
	patterns []string
}{
	{EffectNetwork, []string{"http", "fetch", "axios", "request", "grpc", "socket", "net.", "url."}},
	{EffectDB, []string{"sql", "db.", ".db", "query", "redis", "mongo", "gorm", "database", "repository", "datastore"}},
	{EffectIO, []string{"os.", "io.", "ioutil", "fs.", "file", "open(", "readfile", "writefile", "readall"}},
	{EffectLog, []string{"log", "console", "print", "fmt.print", "slog"}},
}

// sideEffectMutationVerbs mirror the plugins' side-effect verbs — a method name
// containing one is a heuristic mutation signal.
var sideEffectMutationVerbs = []string{"track", "send", "emit", "publish", "save", "create", "update", "delete", "write"}

// ClassifyEffect returns the (kind, basis) for an effect name. A recognized
// category is resolved; a verb-only or unrecognized name is heuristic.
func ClassifyEffect(name string) (kind, basis string) {
	n := strings.ToLower(name)
	for _, cat := range effectCategories {
		for _, p := range cat.patterns {
			if strings.Contains(n, p) {
				return cat.kind, BasisResolved
			}
		}
	}
	for _, v := range sideEffectMutationVerbs {
		if strings.Contains(n, v) {
			return EffectMutation, BasisHeuristic
		}
	}
	return EffectUnclassified, BasisHeuristic
}

// effectBasis prefers an effect's declared basis, then maps a legacy confidence,
// then classifies by name.
func effectBasis(e SideEffect) string {
	if e.Basis != "" {
		return e.Basis
	}
	switch e.Confidence {
	case ConfidenceHigh:
		return BasisResolved
	case ConfidenceLow:
		return BasisHeuristic
	}
	_, basis := ClassifyEffect(e.Name)
	return basis
}
