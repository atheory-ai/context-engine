package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ParseJSON parses strict v1 SemanticPlan JSON. Unknown fields are rejected at
// every level so plugin and client schema drift cannot silently change meaning.
func ParseJSON(data []byte) (*SemanticPlan, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("parse semantic plan: empty document")
	}
	var envelope struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse semantic plan envelope: %w", err)
	}
	if envelope.SchemaVersion != SchemaVersionV1 {
		return nil, fmt.Errorf("parse semantic plan: unsupported schemaVersion %q", envelope.SchemaVersion)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var plan SemanticPlan
	if err := dec.Decode(&plan); err != nil {
		return nil, fmt.Errorf("parse semantic plan: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse semantic plan: unexpected trailing JSON")
		}
		return nil, fmt.Errorf("parse semantic plan: trailing JSON: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	canonicalIntent, err := canonicalIntent(plan.Intent)
	if err != nil {
		return nil, err
	}
	plan.Intent = canonicalIntent
	canonicalize(&plan)
	return &plan, nil
}

// MarshalCanonical returns canonical JSON for a valid plan. Canonicalization
// sorts semantic records by stable ID and normalizes nil slices to empty arrays.
func MarshalCanonical(plan *SemanticPlan) ([]byte, error) {
	if plan == nil {
		return nil, fmt.Errorf("marshal semantic plan: plan is required")
	}
	clonedPlan, err := clone(plan)
	if err != nil {
		return nil, err
	}
	canonicalIntent, err := canonicalIntent(clonedPlan.Intent)
	if err != nil {
		return nil, err
	}
	clonedPlan.Intent = canonicalIntent
	canonicalize(clonedPlan)
	if err := clonedPlan.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(clonedPlan)
	if err != nil {
		return nil, fmt.Errorf("marshal semantic plan: %w", err)
	}
	return raw, nil
}

// UpgradeJSON normalizes a supported document to the latest canonical schema.
// V1 is the first semantic-plan schema; future versions add explicit upgrade
// cases here rather than relying on permissive decoding.
func UpgradeJSON(data []byte) ([]byte, error) {
	plan, err := ParseJSON(data)
	if err != nil {
		return nil, err
	}
	return MarshalCanonical(plan)
}
