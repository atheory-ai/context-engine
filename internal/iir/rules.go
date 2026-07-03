package iir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// projectRulePackNames are the conventional filenames for a project rule pack,
// checked in priority order.
var projectRulePackNames = []string{"iir.rules.yaml", "iir.rules.yml", "iir.rules.json"}

// DiscoverProjectRulePack searches startDir and its ancestors for a project
// rule pack file. It returns the loaded pack and its path when found. found is
// false with a nil error when no project pack exists — the caller then relies on
// the built-in defaults. A pack that exists but fails to load returns found=true
// with the error, so a broken project pack surfaces rather than being ignored.
func DiscoverProjectRulePack(startDir string) (pack RulePack, path string, found bool, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return RulePack{}, "", false, err
	}
	for {
		for _, name := range projectRulePackNames {
			candidate := filepath.Join(dir, name)
			if _, statErr := os.Stat(candidate); statErr == nil {
				loaded, loadErr := LoadRulePackFile(candidate)
				return loaded, candidate, true, loadErr
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return RulePack{}, "", false, nil
		}
		dir = parent
	}
}

// RuleStatus is the outcome of evaluating a rule against an IIR node.
type RuleStatus string

const (
	RulePassed RuleStatus = "passed"
	RuleFailed RuleStatus = "failed"
	RuleWarned RuleStatus = "warned"
	// RuleSkipped means the rule's `when` conditions did not select the node.
	RuleSkipped RuleStatus = "skipped"
)

// Rule is a declarative expectation attached to a class of IIR node. Rules apply
// to semantic objects, not raw text. Slice 1/2 supports FunctionIntent targets.
type Rule struct {
	ID       string      `json:"id" yaml:"id"`
	Target   Kind        `json:"target" yaml:"target"`
	Severity Severity    `json:"severity" yaml:"severity"`
	When     RuleWhen    `json:"when" yaml:"when"`
	Require  RuleRequire `json:"require" yaml:"require"`
}

// RuleWhen narrows which nodes a rule applies to. Empty fields match anything.
type RuleWhen struct {
	Visibility      Visibility `json:"visibility,omitempty" yaml:"visibility,omitempty"`
	HasFailureModes *bool      `json:"hasFailureModes,omitempty" yaml:"hasFailureModes,omitempty"`
}

// RuleRequire is the set of conditions the node must satisfy. Nil pointers mean
// the rule does not assert that condition.
type RuleRequire struct {
	ExplicitReturnType  *bool   `json:"explicitReturnType,omitempty" yaml:"explicitReturnType,omitempty"`
	SideEffectsDeclared *bool   `json:"sideEffectsDeclared,omitempty" yaml:"sideEffectsDeclared,omitempty"`
	FailureStrategy     *string `json:"failureStrategy,omitempty" yaml:"failureStrategy,omitempty"`
}

// RulePack is a named, ordered collection of rules loaded from a file.
type RulePack struct {
	Rules []Rule `json:"rules" yaml:"rules"`
}

// RuleResult is the report entry for one evaluated rule.
type RuleResult struct {
	ID       string     `json:"id"`
	Target   Kind       `json:"target"`
	Severity Severity   `json:"severity"`
	Status   RuleStatus `json:"status"`
	Message  string     `json:"message"`
	Repair   string     `json:"repair,omitempty"`
}

// DefaultRulePack is the built-in defensive rule set. It is always applied as
// the base layer; project rule packs are layered on top (see MergeRulePacks).
// Rules are encoded as executable objects rather than prompt guidance.
func DefaultRulePack() RulePack {
	trueVal := true
	resultType := "ResultType"
	return RulePack{Rules: []Rule{
		{
			ID:       "function-explicit-return-type",
			Target:   KindFunctionIntent,
			Severity: SeverityError,
			When:     RuleWhen{Visibility: VisibilityPublic},
			Require:  RuleRequire{ExplicitReturnType: &trueVal},
		},
		{
			ID:       "declare-side-effects",
			Target:   KindFunctionIntent,
			Severity: SeverityError,
			Require:  RuleRequire{SideEffectsDeclared: &trueVal},
		},
		{
			ID:       "expected-failures-use-result",
			Target:   KindFunctionIntent,
			Severity: SeverityWarning,
			When:     RuleWhen{HasFailureModes: &trueVal},
			Require:  RuleRequire{FailureStrategy: &resultType},
		},
	}}
}

// MergeRulePacks layers override rules onto a base pack. A rule in override with
// the same id as one in base replaces it in place (preserving base ordering);
// override rules with new ids are appended. This lets a project extend or tune
// the built-in defaults without restating them.
func MergeRulePacks(base, override RulePack) RulePack {
	merged := make([]Rule, len(base.Rules))
	copy(merged, base.Rules)

	index := make(map[string]int, len(merged))
	for i, r := range merged {
		index[r.ID] = i
	}
	for _, r := range override.Rules {
		if i, ok := index[r.ID]; ok {
			merged[i] = r
			continue
		}
		index[r.ID] = len(merged)
		merged = append(merged, r)
	}
	return RulePack{Rules: merged}
}

// LoadRulePackFile reads and validates a rule pack from a YAML or JSON file.
func LoadRulePackFile(path string) (RulePack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RulePack{}, fmt.Errorf("read rule pack %s: %w", path, err)
	}
	pack, err := LoadRulePack(data)
	if err != nil {
		return RulePack{}, fmt.Errorf("%s: %w", path, err)
	}
	return pack, nil
}

// LoadRulePack parses and validates a rule pack from raw bytes.
func LoadRulePack(data []byte) (RulePack, error) {
	var pack RulePack
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&pack); err != nil {
		return RulePack{}, fmt.Errorf("parse rule pack: %w", err)
	}
	if err := validateRulePack(pack); err != nil {
		return RulePack{}, err
	}
	return pack, nil
}

func validateRulePack(pack RulePack) error {
	if len(pack.Rules) == 0 {
		return fmt.Errorf("rule pack has no rules")
	}
	seen := map[string]bool{}
	for i, r := range pack.Rules {
		if strings.TrimSpace(r.ID) == "" {
			return fmt.Errorf("rules[%d]: missing id", i)
		}
		if seen[r.ID] {
			return fmt.Errorf("rules[%d]: duplicate id %q", i, r.ID)
		}
		seen[r.ID] = true
		switch r.Target {
		case KindFunctionIntent:
		case "":
			return fmt.Errorf("rule %q: missing target", r.ID)
		default:
			return fmt.Errorf("rule %q: unsupported target %q (Slice 1 supports only %q)",
				r.ID, r.Target, KindFunctionIntent)
		}
		switch r.Severity {
		case SeverityError, SeverityWarning, SeverityInfo:
		case "":
			return fmt.Errorf("rule %q: missing severity", r.ID)
		default:
			return fmt.Errorf("rule %q: unknown severity %q", r.ID, r.Severity)
		}
		if fs := r.Require.FailureStrategy; fs != nil && *fs != "ResultType" {
			return fmt.Errorf("rule %q: unsupported failureStrategy %q (expected \"ResultType\")", r.ID, *fs)
		}
	}
	return nil
}

// EvaluateRules runs every FunctionIntent-targeted rule against the extracted
// intent, deterministically in pack order. The extracted intent is the subject
// because rules describe how code should be written.
func EvaluateRules(pack RulePack, intent *FunctionIntent) []RuleResult {
	results := make([]RuleResult, 0, len(pack.Rules))
	for _, rule := range pack.Rules {
		if rule.Target != KindFunctionIntent {
			continue // Slice 1 only evaluates FunctionIntent rules
		}
		results = append(results, evaluateRule(rule, intent))
	}
	return results
}

func evaluateRule(rule Rule, intent *FunctionIntent) RuleResult {
	res := RuleResult{ID: rule.ID, Target: rule.Target, Severity: rule.Severity}

	if !ruleSelects(rule.When, intent) {
		res.Status = RuleSkipped
		res.Message = "rule conditions did not apply to this function"
		return res
	}

	ok, msg, repair := checkRequire(rule.Require, intent)
	res.Message = msg
	res.Repair = repair
	switch {
	case ok:
		res.Status = RulePassed
	case rule.Severity == SeverityError:
		res.Status = RuleFailed
	default:
		res.Status = RuleWarned
	}
	return res
}

func ruleSelects(when RuleWhen, intent *FunctionIntent) bool {
	if when.Visibility != "" && when.Visibility != intent.Visibility {
		return false
	}
	if when.HasFailureModes != nil && *when.HasFailureModes != intent.HasFailureModes() {
		return false
	}
	return true
}

// checkRequire evaluates the require block. It returns pass/fail plus a message
// and, on failure, repair guidance. Multiple requirements are ANDed; the first
// failing one is reported.
func checkRequire(req RuleRequire, intent *FunctionIntent) (ok bool, msg, repair string) {
	// Booleans are honored in both directions: `true` asserts the property,
	// `false` asserts its absence. A mismatch either way is a failure — never a
	// silent pass.
	if req.ExplicitReturnType != nil && *req.ExplicitReturnType != intent.Returns.Explicit {
		if *req.ExplicitReturnType {
			return false, "function must declare an explicit return type",
				"Add an explicit return type annotation to the function signature."
		}
		return false, "function must not declare an explicit return type",
			"Remove the explicit return type annotation from the function signature."
	}
	if req.SideEffectsDeclared != nil {
		declared := intent.SideEffects != nil
		if *req.SideEffectsDeclared != declared {
			if *req.SideEffectsDeclared {
				return false, "side effects must be declared (use an empty list to assert none)",
					"Declare a sideEffects list in the IIR (empty [] means no side effects)."
			}
			return false, "side effects must not be declared",
				"Remove the sideEffects declaration from the IIR."
		}
	}
	if req.FailureStrategy != nil && *req.FailureStrategy == "ResultType" && intent.HasFailureModes() {
		if returnLooksLikeResult(intent.Returns.Type) {
			return true, "failure modes are surfaced through a result type", ""
		}
		return false, "expected failures should be returned via a Result type, not thrown",
			"Change the return type to a Result/ValidationResult that carries the failure."
	}
	return true, "requirements satisfied", ""
}

// returnLooksLikeResult is a deterministic heuristic for Result-style returns.
func returnLooksLikeResult(returnType string) bool {
	lower := strings.ToLower(returnType)
	return strings.Contains(lower, "result") || strings.Contains(lower, "either") ||
		strings.Contains(lower, "option")
}
