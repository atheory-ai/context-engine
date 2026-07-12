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

// Effect confidence levels. Detection that matches a curated effectful pattern is
// high confidence; a purely heuristic detection is low. The comparator grades an
// undeclared effect's severity by this: high → error, low → warning.
const (
	ConfidenceHigh = "high"
	ConfidenceLow  = "low"
)

// SideEffect is an observable effect a function performs. On the wire it is
// either a bare string (the effect name, e.g. "analytics.track") or an object
// carrying an optional kind and confidence — both forms parse, and a name-only
// effect marshals back to a bare string so existing IIR stays byte-stable.
type SideEffect struct {
	Name       string `json:"name" yaml:"name"`
	Kind       string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Confidence string `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// plain reports whether only the name is set (no kind/confidence), so the effect
// round-trips as a bare string.
func (e SideEffect) plain() bool { return e.Kind == "" && e.Confidence == "" }

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

// effectCategories maps a curated substring to (kind, high confidence). An effect
// name matching one is a recognized, real effect; anything else is heuristic-only
// (low confidence), which the comparator treats as a warning rather than an error.
var effectCategories = []struct {
	kind     string
	patterns []string
}{
	{EffectNetwork, []string{"http", "fetch", "axios", "request", "grpc", "socket", "net.", "url."}},
	{EffectDB, []string{"sql", "db.", ".db", "query", "redis", "mongo", "gorm", "database", "repository", "datastore"}},
	{EffectIO, []string{"os.", "io.", "ioutil", "fs.", "file", "open(", "readfile", "writefile", "readall"}},
	{EffectLog, []string{"log", "console", "print", "fmt.print", "slog"}},
	{EffectMutation, sideEffectMutationVerbs},
}

// sideEffectMutationVerbs mirror the plugins' side-effect verbs — a method name
// containing one signals an observable mutation/effect.
var sideEffectMutationVerbs = []string{"track", "send", "emit", "publish", "save", "create", "update", "delete", "write"}

// ClassifyEffect returns the (kind, confidence) for an effect name using the
// curated category registry. A recognized effect is high confidence; an
// unrecognized one is low-confidence "unclassified".
func ClassifyEffect(name string) (kind, confidence string) {
	n := strings.ToLower(name)
	for _, cat := range effectCategories {
		for _, p := range cat.patterns {
			if strings.Contains(n, p) {
				return cat.kind, ConfidenceHigh
			}
		}
	}
	return EffectUnclassified, ConfidenceLow
}

// effectConfidence prefers an effect's own declared confidence, falling back to
// classification by name.
func effectConfidence(e SideEffect) string {
	if e.Confidence != "" {
		return e.Confidence
	}
	_, conf := ClassifyEffect(e.Name)
	return conf
}
