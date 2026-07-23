// Package decorate compiles a candidate semantic plan with the declarative
// requirements supplied by the plugins that apply to its resolved language.
// It deliberately evaluates data in the host; plugin WASM never mutates a plan.
package decorate

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/atheory-ai/context-engine/internal/semantic/passes"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

const SchemaVersionV1 = "v1"

// Contribution is one plugin's opaque manifest payload plus host-owned origin
// metadata. PluginID and Version are assigned by the runtime, not trusted from
// the JSON document.
type Contribution struct {
	PluginID string
	Version  string
	Raw      []byte
}

// Pack is the plugin SDK wire format. Languages selects the broad target
// language; individual policies can further select on plan claims.
type Pack struct {
	SchemaVersion string          `json:"schemaVersion"`
	Languages     []string        `json:"languages,omitempty"`
	Policies      []passes.Policy `json:"policies"`
}

// Input keeps precedence explicit: built-in policies, selected plugin packs,
// then project policies. Conflicts are handled by passes.MergePolicies rather
// than silently giving the last plugin authority.
type Input struct {
	BuiltIn []passes.Policy
	Plugins []Contribution
	Project []passes.Policy
}

// Result explains the selected plugin policies alongside the immutable plan
// revision they produced. SkippedPluginIDs are normal context selection, not
// policy failures.
type Result struct {
	Plan             *plan.SemanticPlan `json:"plan"`
	Findings         []passes.Finding   `json:"findings"`
	AppliedPluginIDs []string           `json:"appliedPluginIds"`
	SkippedPluginIDs []string           `json:"skippedPluginIds"`
}

// Apply parses, selects, merges, and host-evaluates all relevant contributions.
func Apply(source *plan.SemanticPlan, input Input) (*Result, error) {
	if source == nil {
		return nil, fmt.Errorf("semantic decoration: plan is required")
	}
	if err := source.Validate(); err != nil {
		return nil, fmt.Errorf("semantic decoration: %w", err)
	}

	plugins := append([]Contribution(nil), input.Plugins...)
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].PluginID < plugins[j].PluginID })
	pluginPolicies := make([]passes.Policy, 0)
	applied, skipped := make([]string, 0), make([]string, 0)
	for _, contribution := range plugins {
		if contribution.PluginID == "" {
			return nil, fmt.Errorf("semantic decoration: plugin contribution has no plugin ID")
		}
		pack, err := ParsePack(contribution.Raw)
		if err != nil {
			return nil, fmt.Errorf("semantic decoration: plugin %s: %w", contribution.PluginID, err)
		}
		if !appliesToLanguage(pack, source.Unit.Language) {
			skipped = append(skipped, contribution.PluginID)
			continue
		}
		for index := range pack.Policies {
			pack.Policies[index].Producer = "plugin:" + contribution.PluginID + "@" + contribution.Version
		}
		pluginPolicies = append(pluginPolicies, pack.Policies...)
		applied = append(applied, contribution.PluginID)
	}

	merged, err := passes.MergePolicies(input.BuiltIn, pluginPolicies, input.Project)
	if err != nil {
		return nil, fmt.Errorf("semantic decoration: merge policies: %w", err)
	}
	if len(merged) == 0 {
		return &Result{Plan: source, Findings: []passes.Finding{}, AppliedPluginIDs: applied, SkippedPluginIDs: skipped}, nil
	}
	out, err := passes.Apply(source, merged)
	if err != nil {
		return nil, fmt.Errorf("semantic decoration: apply policies: %w", err)
	}
	return &Result{Plan: out.Plan, Findings: out.Findings, AppliedPluginIDs: applied, SkippedPluginIDs: skipped}, nil
}

// ParsePack validates the outer versioned wire envelope before the host policy
// evaluator validates each policy's semantic fields.
func ParsePack(raw []byte) (Pack, error) {
	if len(raw) == 0 {
		return Pack{}, fmt.Errorf("empty semantic policy pack")
	}
	var pack Pack
	if err := json.Unmarshal(raw, &pack); err != nil {
		return Pack{}, fmt.Errorf("parse semantic policy pack: %w", err)
	}
	if pack.SchemaVersion != SchemaVersionV1 {
		return Pack{}, fmt.Errorf("unsupported schemaVersion %q", pack.SchemaVersion)
	}
	if len(pack.Policies) == 0 {
		return Pack{}, fmt.Errorf("semantic policy pack has no policies")
	}
	return pack, nil
}

func appliesToLanguage(pack Pack, language string) bool {
	if len(pack.Languages) == 0 {
		return true
	}
	for _, candidate := range pack.Languages {
		if candidate == language {
			return true
		}
	}
	return false
}
