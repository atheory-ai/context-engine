// Package plan defines the versioned semantic contract between an intent and a
// renderer. A SemanticPlan is deliberately above source syntax and below an
// implementation recipe: it records what is known, how it is known, what is
// required, and what remains unresolved.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
)

const SchemaVersionV1 = "v1"

// Lifecycle describes where an immutable plan revision sits in the semantic
// compiler pipeline.
type Lifecycle string

const (
	LifecycleDeclared  Lifecycle = "declared"
	LifecycleResolving Lifecycle = "resolving"
	LifecycleResolved  Lifecycle = "resolved"
	LifecycleGenerated Lifecycle = "generated"
	LifecycleObserved  Lifecycle = "observed"
	LifecycleVerified  Lifecycle = "verified"
)

// KnowledgeState records the epistemic state of a semantic record. Confidence
// is evidence metadata; it never changes this state.
type KnowledgeState string

const (
	KnowledgeObserved KnowledgeState = "observed"
	KnowledgeDeclared KnowledgeState = "declared"
	KnowledgeInferred KnowledgeState = "inferred"
	KnowledgeResolved KnowledgeState = "resolved"
	KnowledgeUnknown  KnowledgeState = "unknown"
)

// Confidence communicates how strongly a producer supports an inference. It
// does not turn an inferred claim into an observed claim.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// SemanticPlan is an immutable revision. ParentID links a derived revision to
// the revision it supersedes; revision one has no parent.
type SemanticPlan struct {
	ID            string              `json:"id"`
	ProjectID     core.ProjectID      `json:"projectId"`
	SchemaVersion string              `json:"schemaVersion"`
	Revision      int                 `json:"revision"`
	ParentID      string              `json:"parentId,omitempty"`
	Unit          SemanticUnit        `json:"unit"`
	Intent        *iir.FunctionIntent `json:"intent"`
	Bindings      []SymbolBinding     `json:"bindings"`
	Claims        []SemanticClaim     `json:"claims"`
	Obligations   []Obligation        `json:"obligations"`
	Decisions     []Decision          `json:"decisions"`
	OpenQuestions []OpenQuestion      `json:"openQuestions"`
	PassRecords   []PassRecord        `json:"passRecords"`
	Lifecycle     Lifecycle           `json:"lifecycle"`
	Provenance    []Evidence          `json:"provenance"`
}

// SemanticUnit identifies the graph-backed or provisional unit to which a plan
// applies. At least one of NodeID or CanonicalID is required.
type SemanticUnit struct {
	ID          string      `json:"id"`
	NodeID      core.NodeID `json:"nodeId,omitempty"`
	CanonicalID string      `json:"canonicalId,omitempty"`
	Scope       string      `json:"scope"`
	Language    string      `json:"language"`
	SourceRefs  []SourceRef `json:"sourceRefs"`
}

// SourceRef is evidence anchored to a project-relative source span.
type SourceRef struct {
	Path      string `json:"path"`
	StartByte int    `json:"startByte,omitempty"`
	EndByte   int    `json:"endByte,omitempty"`
}

// Evidence explains where a claim, binding, decision, or plan fact came from.
type Evidence struct {
	ID          string      `json:"id"`
	Source      string      `json:"source"`
	Producer    string      `json:"producer"`
	NodeID      core.NodeID `json:"nodeId,omitempty"`
	SourceRef   *SourceRef  `json:"sourceRef,omitempty"`
	Confidence  Confidence  `json:"confidence,omitempty"`
	ObservedAt  int64       `json:"observedAt,omitempty"`
	Explanation string      `json:"explanation"`
}

// SymbolBinding resolves a semantic role to a graph symbol or preserves its
// unresolved state and evidence.
type SymbolBinding struct {
	ID          string         `json:"id"`
	Role        string         `json:"role"`
	NodeID      core.NodeID    `json:"nodeId,omitempty"`
	CanonicalID string         `json:"canonicalId,omitempty"`
	State       KnowledgeState `json:"state"`
	Evidence    []Evidence     `json:"evidence"`
}

// SemanticClaim is an evidence-bearing statement about a unit or its context.
// Kind and Statement remain intentionally open in v1; later semantic passes
// introduce typed claim vocabularies without changing plan lifecycle rules.
type SemanticClaim struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Statement string         `json:"statement"`
	State     KnowledgeState `json:"state"`
	Evidence  []Evidence     `json:"evidence"`
}

// Obligation is a policy or contract requirement attached to a plan.
type Obligation struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Requirement string         `json:"requirement"`
	State       KnowledgeState `json:"state"`
	Evidence    []Evidence     `json:"evidence"`
}

// Decision records a selected semantic choice and any earlier decisions it
// depends on. Dependencies must form an acyclic graph.
type Decision struct {
	ID         string         `json:"id"`
	QuestionID string         `json:"questionId,omitempty"`
	Value      string         `json:"value"`
	State      KnowledgeState `json:"state"`
	DependsOn  []string       `json:"dependsOn"`
	Evidence   []Evidence     `json:"evidence"`
}

// OpenQuestion represents information needed before a plan can be lowered.
type OpenQuestion struct {
	ID       string         `json:"id"`
	Prompt   string         `json:"prompt"`
	Blocking bool           `json:"blocking"`
	State    KnowledgeState `json:"state"`
	Evidence []Evidence     `json:"evidence"`
}

// PassRecord records the deterministic pass that produced a plan revision.
type PassRecord struct {
	ID       string     `json:"id"`
	PassID   string     `json:"passId"`
	Version  string     `json:"version"`
	Phase    string     `json:"phase"`
	Inputs   []string   `json:"inputs"`
	Outputs  []string   `json:"outputs"`
	Evidence []Evidence `json:"evidence"`
}

var stableID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]*$`)

// NewPlan creates the initial declared revision for a semantic unit.
func NewPlan(projectID core.ProjectID, unit SemanticUnit, intent *iir.FunctionIntent) (*SemanticPlan, error) {
	if intent == nil {
		return nil, fmt.Errorf("new semantic plan: intent is required")
	}
	canonicalIntent, err := canonicalIntent(intent)
	if err != nil {
		return nil, fmt.Errorf("new semantic plan: %w", err)
	}
	if unit.ID == "" {
		unit.ID = "unit"
	}
	if unit.Scope == "" {
		unit.Scope = "function"
	}
	if unit.Language == "" {
		unit.Language = canonicalIntent.Language
	}
	plan := &SemanticPlan{
		ProjectID:     projectID,
		SchemaVersion: SchemaVersionV1,
		Revision:      1,
		Unit:          unit,
		Intent:        canonicalIntent,
		Bindings:      []SymbolBinding{},
		Claims:        []SemanticClaim{},
		Obligations:   []Obligation{},
		Decisions:     []Decision{},
		OpenQuestions: []OpenQuestion{},
		PassRecords:   []PassRecord{},
		Lifecycle:     LifecycleDeclared,
		Provenance: []Evidence{{
			ID:          "intent",
			Source:      "user",
			Producer:    "semantic.plan",
			Confidence:  ConfidenceHigh,
			Explanation: "Initial declared intent.",
		}},
	}
	plan.ID, err = MakePlanID(plan)
	if err != nil {
		return nil, err
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	return plan, nil
}

// NewRevision derives an immutable revision from parent. Parent provenance is
// merged into the successor so a pass cannot erase the evidence history.
func NewRevision(parent, candidate *SemanticPlan) (*SemanticPlan, error) {
	if parent == nil || candidate == nil {
		return nil, fmt.Errorf("new semantic plan revision: parent and candidate are required")
	}
	if err := parent.Validate(); err != nil {
		return nil, fmt.Errorf("new semantic plan revision: invalid parent: %w", err)
	}
	next, err := clone(candidate)
	if err != nil {
		return nil, err
	}
	next.ProjectID = parent.ProjectID
	next.SchemaVersion = parent.SchemaVersion
	next.ParentID = parent.ID
	next.Revision = parent.Revision + 1
	next.Provenance = mergeEvidence(parent.Provenance, next.Provenance)
	next.ID = ""
	next.ID, err = MakePlanID(next)
	if err != nil {
		return nil, err
	}
	if err := next.Validate(); err != nil {
		return nil, err
	}
	return next, nil
}

// MakePlanID deterministically derives an ID from the semantic content of one
// plan revision. The ID and parent link themselves are excluded from the hash.
func MakePlanID(plan *SemanticPlan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("make semantic plan id: plan is required")
	}
	copy, err := clone(plan)
	if err != nil {
		return "", err
	}
	copy.ID = ""
	copy.ParentID = ""
	canonicalIntent, err := canonicalIntent(copy.Intent)
	if err != nil {
		return "", err
	}
	copy.Intent = canonicalIntent
	canonicalize(copy)
	payload, err := json.Marshal(copy)
	if err != nil {
		return "", fmt.Errorf("marshal semantic plan id payload: %w", err)
	}
	h := sha256.Sum256(payload)
	return "plan-" + hex.EncodeToString(h[:16]), nil
}

// Validate checks a plan's schema and lifecycle invariants without changing it.
func (p *SemanticPlan) Validate() error {
	if p == nil {
		return fmt.Errorf("semantic plan is required")
	}
	if !stableID.MatchString(p.ID) {
		return fmt.Errorf("semantic plan id must be a stable local id")
	}
	if strings.TrimSpace(string(p.ProjectID)) == "" {
		return fmt.Errorf("semantic plan projectId is required")
	}
	if p.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("semantic plan schemaVersion %q is unsupported", p.SchemaVersion)
	}
	if p.Revision < 1 {
		return fmt.Errorf("semantic plan revision must be positive")
	}
	if p.Revision == 1 && p.ParentID != "" {
		return fmt.Errorf("semantic plan revision one cannot have a parent")
	}
	if p.Revision > 1 && !stableID.MatchString(p.ParentID) {
		return fmt.Errorf("semantic plan derived revision requires a stable parentId")
	}
	if err := validateUnit(p.Unit); err != nil {
		return err
	}
	if _, err := canonicalIntent(p.Intent); err != nil {
		return fmt.Errorf("semantic plan intent: %w", err)
	}
	if !validLifecycle(p.Lifecycle) {
		return fmt.Errorf("semantic plan lifecycle %q is invalid", p.Lifecycle)
	}
	if err := validateEvidenceSet("semantic plan provenance", p.Provenance, true); err != nil {
		return err
	}
	ids := map[string]struct{}{}
	for _, binding := range p.Bindings {
		if err := reserveID(ids, "binding", binding.ID); err != nil {
			return err
		}
		if strings.TrimSpace(binding.Role) == "" {
			return fmt.Errorf("binding %q role is required", binding.ID)
		}
		if binding.NodeID == "" && strings.TrimSpace(binding.CanonicalID) == "" && binding.State != KnowledgeUnknown {
			return fmt.Errorf("binding %q requires nodeId or canonicalId unless unknown", binding.ID)
		}
		if err := validateStateEvidence("binding", binding.ID, binding.State, binding.Evidence); err != nil {
			return err
		}
	}
	for _, claim := range p.Claims {
		if err := reserveID(ids, "claim", claim.ID); err != nil {
			return err
		}
		if strings.TrimSpace(claim.Kind) == "" || strings.TrimSpace(claim.Statement) == "" {
			return fmt.Errorf("claim %q kind and statement are required", claim.ID)
		}
		if err := validateStateEvidence("claim", claim.ID, claim.State, claim.Evidence); err != nil {
			return err
		}
	}
	for _, obligation := range p.Obligations {
		if err := reserveID(ids, "obligation", obligation.ID); err != nil {
			return err
		}
		if strings.TrimSpace(obligation.Kind) == "" || strings.TrimSpace(obligation.Requirement) == "" {
			return fmt.Errorf("obligation %q kind and requirement are required", obligation.ID)
		}
		if err := validateStateEvidence("obligation", obligation.ID, obligation.State, obligation.Evidence); err != nil {
			return err
		}
	}
	decisionIDs := map[string]Decision{}
	for _, decision := range p.Decisions {
		if err := reserveID(ids, "decision", decision.ID); err != nil {
			return err
		}
		if strings.TrimSpace(decision.Value) == "" {
			return fmt.Errorf("decision %q value is required", decision.ID)
		}
		if err := validateStateEvidence("decision", decision.ID, decision.State, decision.Evidence); err != nil {
			return err
		}
		decisionIDs[decision.ID] = decision
	}
	for _, question := range p.OpenQuestions {
		if err := reserveID(ids, "open question", question.ID); err != nil {
			return err
		}
		if strings.TrimSpace(question.Prompt) == "" {
			return fmt.Errorf("open question %q prompt is required", question.ID)
		}
		if err := validateStateEvidence("open question", question.ID, question.State, question.Evidence); err != nil {
			return err
		}
		if p.Lifecycle == LifecycleResolved && question.Blocking {
			return fmt.Errorf("resolved semantic plan has blocking open question %q", question.ID)
		}
	}
	for _, record := range p.PassRecords {
		if err := reserveID(ids, "pass record", record.ID); err != nil {
			return err
		}
		if strings.TrimSpace(record.PassID) == "" || strings.TrimSpace(record.Version) == "" || strings.TrimSpace(record.Phase) == "" {
			return fmt.Errorf("pass record %q passId, version, and phase are required", record.ID)
		}
		if err := validateEvidenceSet("pass record "+record.ID+" evidence", record.Evidence, false); err != nil {
			return err
		}
	}
	return validateDecisionGraph(decisionIDs)
}

func validateUnit(unit SemanticUnit) error {
	if !stableID.MatchString(unit.ID) {
		return fmt.Errorf("semantic unit id must be a stable local id")
	}
	if unit.NodeID == "" && strings.TrimSpace(unit.CanonicalID) == "" {
		return fmt.Errorf("semantic unit requires nodeId or canonicalId")
	}
	if strings.TrimSpace(unit.Scope) == "" || strings.TrimSpace(unit.Language) == "" {
		return fmt.Errorf("semantic unit scope and language are required")
	}
	for _, ref := range unit.SourceRefs {
		if err := validateSourceRef(ref); err != nil {
			return fmt.Errorf("semantic unit source reference: %w", err)
		}
	}
	return nil
}

func validateStateEvidence(kind, id string, state KnowledgeState, evidence []Evidence) error {
	if !validState(state) {
		return fmt.Errorf("%s %q state %q is invalid", kind, id, state)
	}
	require := state != KnowledgeDeclared
	return validateEvidenceSet(kind+" "+id+" evidence", evidence, require)
}

func validateEvidenceSet(kind string, evidence []Evidence, require bool) error {
	if require && len(evidence) == 0 {
		return fmt.Errorf("%s is required", kind)
	}
	seen := map[string]struct{}{}
	for _, item := range evidence {
		if !stableID.MatchString(item.ID) {
			return fmt.Errorf("%s has invalid evidence id", kind)
		}
		if _, exists := seen[item.ID]; exists {
			return fmt.Errorf("%s has duplicate evidence id %q", kind, item.ID)
		}
		seen[item.ID] = struct{}{}
		if strings.TrimSpace(item.Source) == "" || strings.TrimSpace(item.Producer) == "" || strings.TrimSpace(item.Explanation) == "" {
			return fmt.Errorf("%s evidence %q source, producer, and explanation are required", kind, item.ID)
		}
		if item.Confidence != "" && !validConfidence(item.Confidence) {
			return fmt.Errorf("%s evidence %q confidence %q is invalid", kind, item.ID, item.Confidence)
		}
		if item.SourceRef != nil {
			if err := validateSourceRef(*item.SourceRef); err != nil {
				return fmt.Errorf("%s evidence %q: %w", kind, item.ID, err)
			}
		}
	}
	return nil
}

func validateSourceRef(ref SourceRef) error {
	if strings.TrimSpace(ref.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if ref.StartByte < 0 || ref.EndByte < 0 || (ref.EndByte != 0 && ref.EndByte < ref.StartByte) {
		return fmt.Errorf("byte range is invalid")
	}
	return nil
}

func reserveID(ids map[string]struct{}, kind, id string) error {
	if !stableID.MatchString(id) {
		return fmt.Errorf("%s id must be a stable local id", kind)
	}
	if _, exists := ids[id]; exists {
		return fmt.Errorf("duplicate semantic record id %q", id)
	}
	ids[id] = struct{}{}
	return nil
}

func validateDecisionGraph(decisions map[string]Decision) error {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("decision dependencies contain a cycle at %q", id)
		}
		if visited[id] {
			return nil
		}
		decision := decisions[id]
		visiting[id] = true
		for _, dependency := range decision.DependsOn {
			if _, exists := decisions[dependency]; !exists {
				return fmt.Errorf("decision %q depends on unknown decision %q", id, dependency)
			}
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for id := range decisions {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func validLifecycle(lifecycle Lifecycle) bool {
	switch lifecycle {
	case LifecycleDeclared, LifecycleResolving, LifecycleResolved, LifecycleGenerated, LifecycleObserved, LifecycleVerified:
		return true
	default:
		return false
	}
}

func validState(state KnowledgeState) bool {
	switch state {
	case KnowledgeObserved, KnowledgeDeclared, KnowledgeInferred, KnowledgeResolved, KnowledgeUnknown:
		return true
	default:
		return false
	}
}

func validConfidence(confidence Confidence) bool {
	switch confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		return true
	default:
		return false
	}
}

func canonicalIntent(intent *iir.FunctionIntent) (*iir.FunctionIntent, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent is required")
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return nil, fmt.Errorf("marshal intent: %w", err)
	}
	canonical, err := iir.ParseIntentJSON(raw)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func clone(plan *SemanticPlan) (*SemanticPlan, error) {
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("clone semantic plan: %w", err)
	}
	var copy SemanticPlan
	if err := json.Unmarshal(raw, &copy); err != nil {
		return nil, fmt.Errorf("clone semantic plan: %w", err)
	}
	return &copy, nil
}

func mergeEvidence(parent, candidate []Evidence) []Evidence {
	merged := append([]Evidence{}, parent...)
	seen := make(map[string]struct{}, len(parent))
	for _, evidence := range parent {
		seen[evidence.ID] = struct{}{}
	}
	for _, evidence := range candidate {
		if _, exists := seen[evidence.ID]; exists {
			continue
		}
		seen[evidence.ID] = struct{}{}
		merged = append(merged, evidence)
	}
	return merged
}

func canonicalize(plan *SemanticPlan) {
	plan.Bindings = nonNil(plan.Bindings)
	plan.Claims = nonNil(plan.Claims)
	plan.Obligations = nonNil(plan.Obligations)
	plan.Decisions = nonNil(plan.Decisions)
	plan.OpenQuestions = nonNil(plan.OpenQuestions)
	plan.PassRecords = nonNil(plan.PassRecords)
	plan.Provenance = canonicalEvidence(plan.Provenance)
	plan.Unit.SourceRefs = canonicalSourceRefs(plan.Unit.SourceRefs)
	sort.Slice(plan.Bindings, func(i, j int) bool { return plan.Bindings[i].ID < plan.Bindings[j].ID })
	for i := range plan.Bindings {
		plan.Bindings[i].Evidence = canonicalEvidence(plan.Bindings[i].Evidence)
	}
	sort.Slice(plan.Claims, func(i, j int) bool { return plan.Claims[i].ID < plan.Claims[j].ID })
	for i := range plan.Claims {
		plan.Claims[i].Evidence = canonicalEvidence(plan.Claims[i].Evidence)
	}
	sort.Slice(plan.Obligations, func(i, j int) bool { return plan.Obligations[i].ID < plan.Obligations[j].ID })
	for i := range plan.Obligations {
		plan.Obligations[i].Evidence = canonicalEvidence(plan.Obligations[i].Evidence)
	}
	sort.Slice(plan.Decisions, func(i, j int) bool { return plan.Decisions[i].ID < plan.Decisions[j].ID })
	for i := range plan.Decisions {
		plan.Decisions[i].DependsOn = nonNil(plan.Decisions[i].DependsOn)
		sort.Strings(plan.Decisions[i].DependsOn)
		plan.Decisions[i].Evidence = canonicalEvidence(plan.Decisions[i].Evidence)
	}
	sort.Slice(plan.OpenQuestions, func(i, j int) bool { return plan.OpenQuestions[i].ID < plan.OpenQuestions[j].ID })
	for i := range plan.OpenQuestions {
		plan.OpenQuestions[i].Evidence = canonicalEvidence(plan.OpenQuestions[i].Evidence)
	}
	sort.Slice(plan.PassRecords, func(i, j int) bool { return plan.PassRecords[i].ID < plan.PassRecords[j].ID })
	for i := range plan.PassRecords {
		plan.PassRecords[i].Inputs = nonNil(plan.PassRecords[i].Inputs)
		plan.PassRecords[i].Outputs = nonNil(plan.PassRecords[i].Outputs)
		sort.Strings(plan.PassRecords[i].Inputs)
		sort.Strings(plan.PassRecords[i].Outputs)
		plan.PassRecords[i].Evidence = canonicalEvidence(plan.PassRecords[i].Evidence)
	}
}

func canonicalEvidence(evidence []Evidence) []Evidence {
	evidence = nonNil(evidence)
	for i := range evidence {
		if evidence[i].SourceRef != nil {
			ref := *evidence[i].SourceRef
			evidence[i].SourceRef = &ref
		}
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].ID < evidence[j].ID })
	return evidence
}

func canonicalSourceRefs(refs []SourceRef) []SourceRef {
	refs = nonNil(refs)
	sort.Slice(refs, func(i, j int) bool {
		left, right := refs[i], refs[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.StartByte != right.StartByte {
			return left.StartByte < right.StartByte
		}
		return left.EndByte < right.EndByte
	})
	return refs
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// StableRecordID creates a local ID for callers that need a deterministic,
// human-readable prefix plus collision-resistant content identity.
func StableRecordID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	clean := strings.Trim(strings.ToLower(prefix), "-_.:")
	if clean == "" || !stableID.MatchString(clean+"a") {
		clean = "record"
	}
	return clean + "-" + hex.EncodeToString(h[:8]) + strconv.Itoa(len(parts))
}
