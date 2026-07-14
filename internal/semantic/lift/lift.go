// Package lift owns host validation and canonicalization of the plan-aware
// plugin source-lift contract. Plugins only bind language syntax to this
// contract; they do not execute semantic transformations in the host.
package lift

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
)

const SchemaVersionV1 = "v1"

// Coverage is intentionally separate from the plugin package version. Only a
// modeled observation can satisfy a mandatory semantic verification obligation.
type Coverage string

const (
	CoverageModeled     Coverage = "modeled"
	CoveragePartial     Coverage = "partial"
	CoverageUnsupported Coverage = "unsupported"
)

type Evidence struct {
	Path      string `json:"path,omitempty"`
	StartByte int    `json:"startByte,omitempty"`
	EndByte   int    `json:"endByte,omitempty"`
	Basis     string `json:"basis,omitempty"`
}

type Claim struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Statement string     `json:"statement"`
	Evidence  []Evidence `json:"evidence"`
}

// Unit is the canonical observed-semantic output of one plugin lift.
type Unit struct {
	NodeID        core.NodeID         `json:"nodeId"`
	Language      string              `json:"language"`
	SchemaVersion string              `json:"schemaVersion"`
	Observed      *iir.FunctionIntent `json:"observedIntent"`
	Claims        []Claim             `json:"claims"`
	Evidence      []Evidence          `json:"evidence"`
	Coverage      Coverage            `json:"coverage"`
}

// Capability declares only the semantic contract a language plugin can model.
// It neither loads a plugin nor adds another runtime.
type Capability struct {
	Language       string     `json:"language"`
	SchemaVersions []string   `json:"schemaVersions"`
	Coverage       []Coverage `json:"coverage"`
	Claims         []string   `json:"claims"`
}

// CapabilityFor is the v1 host capability matrix. Languages using the legacy
// intent-only payload remain compatible through partial coverage.
func CapabilityFor(language string) Capability {
	return Capability{Language: language, SchemaVersions: []string{SchemaVersionV1}, Coverage: []Coverage{CoverageModeled, CoveragePartial, CoverageUnsupported}, Claims: []string{"effect.*", "failure.*", "unsupported", "unknown"}}
}

// Normalize parses, validates, and canonicalizes one plugin payload. Missing
// plan-aware fields from an older plugin become an explicit partial result;
// invalid or unsupported payloads return an error so callers skip semantic
// storage while allowing structural indexing to continue.
func Normalize(extracted core.IIRExtracted) (*Unit, error) {
	if extracted.NodeID == "" {
		return nil, fmt.Errorf("source lift: nodeId is required")
	}
	intent, err := iir.ParseIntentJSON(extracted.Intent)
	if err != nil {
		return nil, fmt.Errorf("source lift %s intent: %w", extracted.NodeID, err)
	}
	if intent.Origin != iir.OriginObserved {
		return nil, fmt.Errorf("source lift %s intent must have observed origin", extracted.NodeID)
	}
	version := extracted.SchemaVersion
	coverage := Coverage(extracted.Coverage)
	if version == "" && coverage == "" && len(extracted.Claims) == 0 && len(extracted.Evidence) == 0 {
		version, coverage = SchemaVersionV1, CoveragePartial
	}
	if version != SchemaVersionV1 {
		return nil, fmt.Errorf("source lift %s unsupported schema version %q", extracted.NodeID, version)
	}
	if coverage != CoverageModeled && coverage != CoveragePartial && coverage != CoverageUnsupported {
		return nil, fmt.Errorf("source lift %s has invalid coverage %q", extracted.NodeID, coverage)
	}
	claims := claimsFrom(extracted.Claims)
	evidence := evidenceFrom(extracted.Evidence)
	if coverage != CoverageModeled && !hasCoverageClaim(claims) {
		kind := "unknown"
		if coverage == CoverageUnsupported {
			kind = "unsupported"
		}
		claims = append(claims, Claim{ID: "lift-coverage", Kind: kind, Statement: "Plugin lift coverage is " + string(coverage) + ".", Evidence: evidence})
	}
	if coverage == CoverageModeled && len(evidence) == 0 && len(claims) == 0 {
		return nil, fmt.Errorf("source lift %s modeled coverage requires source evidence or claims", extracted.NodeID)
	}
	unit := &Unit{NodeID: extracted.NodeID, Language: intent.Language, SchemaVersion: version, Observed: intent, Claims: claims, Evidence: evidence, Coverage: coverage}
	canonicalize(unit)
	return unit, nil
}

// CanSatisfyMandatory is the verification guard: absence or partial source
// observation is never a passing proof for a mandatory obligation.
func (u *Unit) CanSatisfyMandatory() bool { return u != nil && u.Coverage == CoverageModeled }

func claimsFrom(raw []core.IIRClaim) []Claim {
	claims := make([]Claim, 0, len(raw))
	seen := map[string]struct{}{}
	for _, claim := range raw {
		if strings.TrimSpace(claim.ID) == "" || strings.TrimSpace(claim.Kind) == "" || strings.TrimSpace(claim.Statement) == "" {
			continue
		}
		if _, duplicate := seen[claim.ID]; duplicate {
			continue
		}
		seen[claim.ID] = struct{}{}
		claims = append(claims, Claim{ID: claim.ID, Kind: claim.Kind, Statement: claim.Statement, Evidence: evidenceFrom(claim.Evidence)})
	}
	return claims
}

func evidenceFrom(raw []core.IIRSourceEvidence) []Evidence {
	evidence := make([]Evidence, 0, len(raw))
	for _, item := range raw {
		if item.StartByte < 0 || item.EndByte < item.StartByte {
			continue
		}
		evidence = append(evidence, Evidence{Path: item.Path, StartByte: item.StartByte, EndByte: item.EndByte, Basis: item.Basis})
	}
	return evidence
}

func hasCoverageClaim(claims []Claim) bool {
	for _, claim := range claims {
		if claim.Kind == "unknown" || claim.Kind == "unsupported" {
			return true
		}
	}
	return false
}

func canonicalize(unit *Unit) {
	sort.Slice(unit.Claims, func(i, j int) bool { return unit.Claims[i].ID < unit.Claims[j].ID })
	for index := range unit.Claims {
		sort.Slice(unit.Claims[index].Evidence, func(i, j int) bool {
			return evidenceKey(unit.Claims[index].Evidence[i]) < evidenceKey(unit.Claims[index].Evidence[j])
		})
	}
	sort.Slice(unit.Evidence, func(i, j int) bool { return evidenceKey(unit.Evidence[i]) < evidenceKey(unit.Evidence[j]) })
	if unit.Claims == nil {
		unit.Claims = []Claim{}
	}
	if unit.Evidence == nil {
		unit.Evidence = []Evidence{}
	}
}

func evidenceKey(item Evidence) string {
	return fmt.Sprintf("%s:%012d:%012d:%s", item.Path, item.StartByte, item.EndByte, item.Basis)
}
