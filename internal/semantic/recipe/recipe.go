// Package recipe deterministically lowers resolved semantic plans into compact
// renderer contracts. It has no model, plugin-runtime, graph, or storage
// dependency: a renderer receives only the recipe it is asked to realize.
package recipe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

const SchemaVersionV1 = "v1"

// ImplementationRecipe is the canonical, generator-facing result of lowering
// one resolved semantic-plan revision. Its items carry source-plan record IDs
// rather than copied graph context or hidden architectural decisions.
type ImplementationRecipe struct {
	ID                  string          `json:"id"`
	SchemaVersion       string          `json:"schemaVersion"`
	PlanRevisionID      string          `json:"planRevisionId"`
	TargetLanguage      string          `json:"targetLanguage"`
	Target              Target          `json:"target"`
	Imports             []Import        `json:"imports"`
	Signature           Signature       `json:"signature"`
	Steps               []Step          `json:"steps"`
	Effects             []Effect        `json:"effects"`
	Failures            []Failure       `json:"failures"`
	Constraints         []Constraint    `json:"constraints"`
	RendererProfile     RendererProfile `json:"rendererProfile"`
	EvidenceRefs        []string        `json:"evidenceRefs"`
	UnresolvedQuestions []string        `json:"unresolvedQuestions"`
}

type Target struct {
	UnitID      string `json:"unitId"`
	CanonicalID string `json:"canonicalId,omitempty"`
	Mode        string `json:"mode"` // new | existing
}

type Import struct {
	Symbol       string   `json:"symbol"`
	ImportForm   string   `json:"importForm"`
	PlanRecordID string   `json:"planRecordId"`
	EvidenceRefs []string `json:"evidenceRefs"`
}

type Parameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Signature struct {
	Name         string      `json:"name"`
	Visibility   string      `json:"visibility"`
	Parameters   []Parameter `json:"parameters"`
	ReturnType   string      `json:"returnType"`
	PlanRecordID string      `json:"planRecordId"`
}

type Step struct {
	Order        int    `json:"order"`
	Operation    string `json:"operation"`
	RequiredCall string `json:"requiredCall,omitempty"`
	PlanRecordID string `json:"planRecordId"`
}

type Effect struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Required     bool     `json:"required"`
	Forbidden    bool     `json:"forbidden"`
	PlanRecordID string   `json:"planRecordId"`
	EvidenceRefs []string `json:"evidenceRefs"`
}

type Failure struct {
	Code         string `json:"code"`
	Strategy     string `json:"strategy"`
	Source       string `json:"source,omitempty"`
	PlanRecordID string `json:"planRecordId"`
}

type Constraint struct {
	Kind         string `json:"kind"`
	Requirement  string `json:"requirement"`
	Polarity     string `json:"polarity"` // required | forbidden
	PlanRecordID string `json:"planRecordId"`
}

// RendererProfile contains only language facts explicitly selected by the host
// or target plugin. It is realization guidance, not a hidden source template.
type RendererProfile struct {
	Language        string `json:"language"`
	ImportStyle     string `json:"importStyle"`
	AsyncFunctions  bool   `json:"asyncFunctions"`
	TypedParameters bool   `json:"typedParameters"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	PlanID  string `json:"planId"`
}

// Renderer is deliberately recipe-only. An LLM-backed implementation belongs
// outside this package and must return the exact recipe ID in its metadata.
type Renderer interface {
	Supports(*ImplementationRecipe) bool
	Render(context.Context, *ImplementationRecipe) (RenderResult, error)
}

type RenderResult struct {
	Source   string `json:"source"`
	RecipeID string `json:"recipeId"`
	Renderer string `json:"renderer"`
}

// Lower produces a canonical recipe only from a fully resolved plan. A profile
// for an unsupported language is a diagnostic, never a model guess.
func Lower(source *plan.SemanticPlan, profile RendererProfile) (*ImplementationRecipe, []Diagnostic, error) {
	if source == nil {
		return nil, nil, fmt.Errorf("recipe lowering: semantic plan is required")
	}
	if err := source.Validate(); err != nil {
		return nil, nil, fmt.Errorf("recipe lowering: %w", err)
	}
	if source.Lifecycle != plan.LifecycleResolved {
		return nil, nil, fmt.Errorf("recipe lowering: plan %q must be resolved", source.ID)
	}
	if blockingQuestions(source.OpenQuestions) {
		return nil, nil, fmt.Errorf("recipe lowering: plan %q has blocking questions", source.ID)
	}
	if profile.Language == "" {
		profile = DefaultProfile(source.Unit.Language)
	}
	if profile.Language != source.Unit.Language {
		return nil, []Diagnostic{{Code: "unsupported_profile", Message: "renderer profile language does not match the plan target language", PlanID: source.ID}}, fmt.Errorf("recipe lowering: profile language %q does not match %q", profile.Language, source.Unit.Language)
	}
	if profile.ImportStyle == "" {
		return nil, []Diagnostic{{Code: "unsupported_lowering", Message: "target language has no declared import lowering profile", PlanID: source.ID}}, fmt.Errorf("recipe lowering: unsupported target language %q", source.Unit.Language)
	}

	intentRecord := intentRecordID(source)
	recipe := &ImplementationRecipe{
		SchemaVersion:       SchemaVersionV1,
		PlanRevisionID:      source.ID,
		TargetLanguage:      source.Unit.Language,
		Target:              targetFor(source),
		Imports:             importsFor(source, profile),
		Signature:           signatureFor(source.Intent, intentRecord),
		Steps:               stepsFor(source, intentRecord),
		Effects:             effectsFor(source, intentRecord),
		Failures:            failuresFor(source, intentRecord),
		Constraints:         constraintsFor(source),
		RendererProfile:     profile,
		EvidenceRefs:        evidenceRefs(source),
		UnresolvedQuestions: []string{},
	}
	if err := canonicalize(recipe); err != nil {
		return nil, nil, err
	}
	id, err := recipeID(recipe)
	if err != nil {
		return nil, nil, err
	}
	recipe.ID = id
	return recipe, []Diagnostic{}, nil
}

// DefaultProfile is a host-declared capability profile. V1 has an explicit
// TypeScript profile only; other targets produce a diagnostic rather than a
// speculative recipe.
func DefaultProfile(language string) RendererProfile {
	if language == "typescript" {
		return RendererProfile{Language: "typescript", ImportStyle: "named", AsyncFunctions: true, TypedParameters: true}
	}
	return RendererProfile{Language: language}
}

// MarshalCanonical validates and emits byte-stable JSON for storage or a
// renderer request. No raw graph data is ever present in this wire contract.
func MarshalCanonical(recipe *ImplementationRecipe) ([]byte, error) {
	if recipe == nil {
		return nil, fmt.Errorf("canonical recipe: recipe is required")
	}
	clonedRecipe := *recipe
	if err := canonicalize(&clonedRecipe); err != nil {
		return nil, err
	}
	if clonedRecipe.ID == "" {
		id, err := recipeID(&clonedRecipe)
		if err != nil {
			return nil, err
		}
		clonedRecipe.ID = id
	}
	return json.Marshal(clonedRecipe)
}

func targetFor(source *plan.SemanticPlan) Target {
	mode := "new"
	if source.Unit.NodeID != "" {
		mode = "existing"
	}
	return Target{UnitID: source.Unit.ID, CanonicalID: source.Unit.CanonicalID, Mode: mode}
}

func importsFor(source *plan.SemanticPlan, profile RendererProfile) []Import {
	imports := make([]Import, 0, len(source.Bindings))
	for _, binding := range source.Bindings {
		if binding.State != plan.KnowledgeResolved || binding.CanonicalID == "" {
			continue
		}
		imports = append(imports, Import{Symbol: binding.CanonicalID, ImportForm: profile.ImportStyle, PlanRecordID: binding.ID, EvidenceRefs: evidenceIDs(binding.Evidence)})
	}
	return imports
}

func signatureFor(intent *iir.FunctionIntent, recordID string) Signature {
	parameters := make([]Parameter, 0, len(intent.Inputs))
	for _, input := range intent.Inputs {
		parameters = append(parameters, Parameter{Name: input.Name, Type: input.Type})
	}
	return Signature{Name: intent.Name, Visibility: string(intent.Visibility), Parameters: parameters, ReturnType: intent.Returns.Type, PlanRecordID: recordID}
}

func stepsFor(source *plan.SemanticPlan, intentRecord string) []Step {
	steps := make([]Step, 0, len(source.Intent.Behavior)+len(source.Bindings))
	order := 1
	for _, behavior := range source.Intent.Behavior {
		operation := strings.TrimSpace(behavior.Then)
		if operation == "" && behavior.ThenExpr != nil {
			operation = behavior.ThenExpr.Op
		}
		if operation == "" {
			continue
		}
		steps = append(steps, Step{Order: order, Operation: operation, PlanRecordID: intentRecord})
		order++
	}
	for _, binding := range source.Bindings {
		if binding.State == plan.KnowledgeResolved && binding.CanonicalID != "" {
			steps = append(steps, Step{Order: order, Operation: "boundary call", RequiredCall: binding.CanonicalID, PlanRecordID: binding.ID})
			order++
		}
	}
	return steps
}

func effectsFor(source *plan.SemanticPlan, intentRecord string) []Effect {
	effects := make([]Effect, 0, len(source.Intent.SideEffects)+len(source.Obligations))
	for _, effect := range source.Intent.SideEffects {
		kind := effect.Kind
		if kind == "" {
			kind, _ = iir.ClassifyEffect(effect.Name)
		}
		effects = append(effects, Effect{Name: effect.Name, Kind: kind, Required: true, PlanRecordID: claimFor(source.Claims, "effect."+kind, effect.Name, intentRecord), EvidenceRefs: evidenceForClaim(source.Claims, "effect."+kind, effect.Name)})
	}
	for _, obligation := range source.Obligations {
		if obligation.Kind == "audit" {
			effects = append(effects, Effect{Name: obligation.Requirement, Kind: "audit", Required: true, PlanRecordID: obligation.ID, EvidenceRefs: evidenceIDs(obligation.Evidence)})
		}
	}
	return effects
}

func failuresFor(source *plan.SemanticPlan, intentRecord string) []Failure {
	failures := make([]Failure, 0, len(source.Intent.FailureModes))
	for _, failure := range source.Intent.FailureModes {
		failures = append(failures, Failure{Code: failure.Code, Strategy: failure.Kind, Source: failure.Source, PlanRecordID: claimFor(source.Claims, "failure."+failure.Kind, failure.Code, intentRecord)})
	}
	for _, obligation := range source.Obligations {
		if strings.Contains(strings.ToLower(obligation.Kind), "failure") || strings.Contains(strings.ToLower(obligation.Requirement), "wrap") {
			failures = append(failures, Failure{Code: obligation.Requirement, Strategy: "policy", PlanRecordID: obligation.ID})
		}
	}
	return failures
}

func constraintsFor(source *plan.SemanticPlan) []Constraint {
	constraints := make([]Constraint, 0, len(source.Obligations)+len(source.Intent.Constraints))
	for _, obligation := range source.Obligations {
		polarity := "required"
		if strings.HasPrefix(strings.ToLower(obligation.Kind), "forbid") || strings.HasPrefix(strings.ToLower(obligation.Requirement), "forbid ") {
			polarity = "forbidden"
		}
		constraints = append(constraints, Constraint{Kind: obligation.Kind, Requirement: obligation.Requirement, Polarity: polarity, PlanRecordID: obligation.ID})
	}
	for _, constraint := range source.Intent.Constraints {
		constraints = append(constraints, Constraint{Kind: "intent", Requirement: constraint, Polarity: "required", PlanRecordID: intentRecordID(source)})
	}
	return constraints
}

func intentRecordID(source *plan.SemanticPlan) string {
	for _, evidence := range source.Provenance {
		if evidence.Field == "intent" {
			return evidence.ID
		}
	}
	return "intent"
}

func claimFor(claims []plan.SemanticClaim, kind, statement, fallback string) string {
	for _, claim := range claims {
		if claim.Kind == kind && claim.Statement == statement {
			return claim.ID
		}
	}
	return fallback
}

func evidenceForClaim(claims []plan.SemanticClaim, kind, statement string) []string {
	for _, claim := range claims {
		if claim.Kind == kind && claim.Statement == statement {
			return evidenceIDs(claim.Evidence)
		}
	}
	return []string{}
}

func evidenceIDs(evidence []plan.Evidence) []string {
	ids := make([]string, 0, len(evidence))
	for _, item := range evidence {
		ids = append(ids, item.ID)
	}
	return ids
}

func evidenceRefs(source *plan.SemanticPlan) []string {
	refs := evidenceIDs(source.Provenance)
	for _, binding := range source.Bindings {
		refs = append(refs, evidenceIDs(binding.Evidence)...)
	}
	for _, claim := range source.Claims {
		refs = append(refs, evidenceIDs(claim.Evidence)...)
	}
	for _, obligation := range source.Obligations {
		refs = append(refs, evidenceIDs(obligation.Evidence)...)
	}
	return refs
}

func blockingQuestions(questions []plan.OpenQuestion) bool {
	for _, question := range questions {
		if question.Blocking {
			return true
		}
	}
	return false
}

func canonicalize(recipe *ImplementationRecipe) error {
	if recipe.SchemaVersion != SchemaVersionV1 || recipe.PlanRevisionID == "" || recipe.TargetLanguage == "" || recipe.Target.UnitID == "" || recipe.Signature.Name == "" {
		return fmt.Errorf("canonical recipe: required fields are missing")
	}
	if len(recipe.UnresolvedQuestions) > 0 {
		return fmt.Errorf("canonical recipe: unresolved questions cannot reach a renderer")
	}
	sort.Slice(recipe.Imports, func(i, j int) bool { return recipe.Imports[i].PlanRecordID < recipe.Imports[j].PlanRecordID })
	sort.Slice(recipe.Effects, func(i, j int) bool {
		return recipe.Effects[i].PlanRecordID+recipe.Effects[i].Name < recipe.Effects[j].PlanRecordID+recipe.Effects[j].Name
	})
	sort.Slice(recipe.Failures, func(i, j int) bool {
		return recipe.Failures[i].PlanRecordID+recipe.Failures[i].Code < recipe.Failures[j].PlanRecordID+recipe.Failures[j].Code
	})
	sort.Slice(recipe.Constraints, func(i, j int) bool { return recipe.Constraints[i].PlanRecordID < recipe.Constraints[j].PlanRecordID })
	sort.Strings(recipe.EvidenceRefs)
	recipe.Imports = nonNil(recipe.Imports)
	recipe.Signature.Parameters = nonNil(recipe.Signature.Parameters)
	recipe.Steps = nonNil(recipe.Steps)
	recipe.Effects = nonNil(recipe.Effects)
	recipe.Failures = nonNil(recipe.Failures)
	recipe.Constraints = nonNil(recipe.Constraints)
	recipe.EvidenceRefs = nonNil(recipe.EvidenceRefs)
	recipe.UnresolvedQuestions = nonNil(recipe.UnresolvedQuestions)
	return nil
}

func recipeID(recipe *ImplementationRecipe) (string, error) {
	clonedRecipe := *recipe
	clonedRecipe.ID = ""
	if err := canonicalize(&clonedRecipe); err != nil {
		return "", err
	}
	payload, err := json.Marshal(clonedRecipe)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(payload)
	return "recipe-" + hex.EncodeToString(hash[:16]), nil
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// TypeScriptEmitter is a deterministic, intentionally small renderer used as
// a lowering oracle. Production model renderers remain separate.
type TypeScriptEmitter struct{}

func (TypeScriptEmitter) Supports(recipe *ImplementationRecipe) bool {
	return recipe != nil && recipe.TargetLanguage == "typescript" && recipe.RendererProfile.ImportStyle != ""
}

func (TypeScriptEmitter) Render(_ context.Context, recipe *ImplementationRecipe) (RenderResult, error) {
	if !(TypeScriptEmitter{}).Supports(recipe) {
		return RenderResult{}, fmt.Errorf("typescript emitter does not support recipe")
	}
	if _, err := MarshalCanonical(recipe); err != nil {
		return RenderResult{}, err
	}
	var source strings.Builder
	for _, imported := range recipe.Imports {
		fmt.Fprintf(&source, "import { %s } from %q;\n", imported.Symbol, imported.Symbol)
	}
	if len(recipe.Imports) > 0 {
		source.WriteByte('\n')
	}
	parameters := make([]string, 0, len(recipe.Signature.Parameters))
	for _, parameter := range recipe.Signature.Parameters {
		parameters = append(parameters, parameter.Name+": "+parameter.Type)
	}
	fmt.Fprintf(&source, "export async function %s(%s): Promise<%s> {\n", recipe.Signature.Name, strings.Join(parameters, ", "), recipe.Signature.ReturnType)
	for _, step := range recipe.Steps {
		if step.RequiredCall != "" {
			fmt.Fprintf(&source, "  await %s();\n", step.RequiredCall)
		}
	}
	for _, effect := range recipe.Effects {
		if effect.Required && !effect.Forbidden {
			fmt.Fprintf(&source, "  await %s();\n", effect.Name)
		}
	}
	for _, failure := range recipe.Failures {
		if failure.Strategy != "policy" {
			fmt.Fprintf(&source, "  throw new Error(%q);\n", failure.Code)
		}
	}
	if recipe.Signature.ReturnType != "void" {
		fmt.Fprintf(&source, "  return undefined as unknown as %s;\n", recipe.Signature.ReturnType)
	}
	source.WriteString("}\n")
	return RenderResult{Source: source.String(), RecipeID: recipe.ID, Renderer: "typescript.deterministic.v1"}, nil
}
