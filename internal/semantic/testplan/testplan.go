// Package testplan lowers resolved semantic plans into traceable test
// expectations. It intentionally does not inspect generated source.
package testplan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
	"github.com/atheory-ai/context-engine/internal/semantic/repair"
)

const SchemaVersionV1 = "v1"

type CoverageStatus string

const (
	CoverageCovered        CoverageStatus = "covered"
	CoverageNotGeneratable CoverageStatus = "not_generatable"
	CoverageUnknown        CoverageStatus = "unknown"
)

type TestCase struct {
	ID               string   `json:"id"`
	Category         string   `json:"category"`
	Preconditions    []string `json:"preconditions"`
	Action           string   `json:"action"`
	ExpectedOutcome  string   `json:"expectedOutcome"`
	RequiredEffects  []string `json:"requiredEffects"`
	ForbiddenEffects []string `json:"forbiddenEffects"`
	ExpectedFailures []string `json:"expectedFailures"`
	SourceClaimIDs   []string `json:"sourceClaimIds"`
	ObligationIDs    []string `json:"obligationIds"`
}

// EvidenceState keeps planned coverage separate from facts observed after a
// rendered test has actually executed. Lowering deliberately starts at
// unknown: emitting a test file is not evidence that it was executable,
// passing, or semantically adequate.
type EvidenceState string

const (
	EvidenceUnknown EvidenceState = "unknown"
	EvidenceYes     EvidenceState = "yes"
	EvidenceNo      EvidenceState = "no"
)

type ExecutionStatus string

const (
	ExecutionNotRun ExecutionStatus = "not_run"
	ExecutionPassed ExecutionStatus = "passed"
	ExecutionFailed ExecutionStatus = "failed"
)

// RenderTypeScript emits a compact Vitest skeleton from test expectations. It
// does not claim execution or semantic coverage; those remain explicit fields
// on the test plan.
func RenderTypeScript(testPlan *Plan) (string, error) {
	if testPlan == nil || testPlan.TargetLanguage != "typescript" {
		return "", fmt.Errorf("test-plan render: TypeScript plan required")
	}
	var source strings.Builder
	source.WriteString("import { describe, it, expect } from \"vitest\";\n\n")
	source.WriteString("describe(\"semantic plan " + testPlan.PlanRevisionID + "\", () => {\n")
	for _, testCase := range testPlan.Cases {
		fmt.Fprintf(&source, "  it(%q, async () => {\n", testCase.Category+": "+testCase.ExpectedOutcome)
		if len(testCase.RequiredEffects) > 0 {
			fmt.Fprintf(&source, "    // required effects: %s\n", strings.Join(testCase.RequiredEffects, ", "))
		}
		if len(testCase.ExpectedFailures) > 0 {
			fmt.Fprintf(&source, "    // expected failures: %s\n", strings.Join(testCase.ExpectedFailures, ", "))
		}
		source.WriteString("    expect(true).toBe(true);\n  });\n")
	}
	source.WriteString("});\n")
	return source.String(), nil
}

type CoverageExpectation struct {
	PlanRecordID        string          `json:"planRecordId"`
	Status              CoverageStatus  `json:"status"`
	TestCaseIDs         []string        `json:"testCaseIds"`
	Emitted             EvidenceState   `json:"emitted"`
	Executable          EvidenceState   `json:"executable"`
	Execution           ExecutionStatus `json:"execution"`
	SemanticallyCovered EvidenceState   `json:"semanticallyCovered"`
	Explanation         string          `json:"explanation,omitempty"`
}
type VerificationGap struct {
	PlanRecordID string `json:"planRecordId"`
	Reason       string `json:"reason"`
}
type Plan struct {
	ID               string                `json:"id"`
	SchemaVersion    string                `json:"schemaVersion"`
	PlanRevisionID   string                `json:"planRevisionId"`
	TargetLanguage   string                `json:"targetLanguage"`
	FrameworkProfile string                `json:"frameworkProfile"`
	Cases            []TestCase            `json:"cases"`
	Coverage         []CoverageExpectation `json:"coverage"`
	Gaps             []VerificationGap     `json:"gaps"`
}

func Lower(source *plan.SemanticPlan, lowered *recipe.ImplementationRecipe) (*Plan, error) {
	return LowerWithRepairs(source, lowered, nil)
}

// LowerWithRepairs adds regression cases for actionable implementation repair
// findings. Repair plans remain immutable input; a subsequent semantic-plan
// revision can choose whether to apply them.
func LowerWithRepairs(source *plan.SemanticPlan, lowered *recipe.ImplementationRecipe, repairs []repair.Plan) (*Plan, error) {
	if source == nil || lowered == nil || source.ID != lowered.PlanRevisionID {
		return nil, fmt.Errorf("test-plan lowering: matching plan and recipe required")
	}
	if source.Lifecycle != plan.LifecycleResolved {
		return nil, fmt.Errorf("test-plan lowering: plan must be resolved")
	}
	out := &Plan{SchemaVersion: SchemaVersionV1, PlanRevisionID: source.ID, TargetLanguage: source.Unit.Language, FrameworkProfile: "vitest", Cases: []TestCase{}, Coverage: []CoverageExpectation{}, Gaps: []VerificationGap{}}
	// A resolved intent itself is a mandatory semantic claim, even when the
	// plan has no separately materialized claims yet.
	intentID := "intent"
	addCase(out, TestCase{ID: "test-" + intentID, Category: "nominal behavior", Preconditions: []string{}, Action: "invoke " + source.Intent.Name, ExpectedOutcome: "returns " + source.Intent.Returns.Type, RequiredEffects: []string{}, ForbiddenEffects: []string{}, ExpectedFailures: []string{}, SourceClaimIDs: []string{intentID}, ObligationIDs: []string{}})
	addCoverage(out, intentID, CoverageCovered, "", "test-"+intentID)
	for _, claim := range source.Claims {
		addCoverage(out, claim.ID, CoverageUnknown, "No target-independent case was derivable for this claim.")
	}
	for _, effect := range lowered.Effects {
		if effect.Required {
			addCase(out, TestCase{ID: "test-" + effect.PlanRecordID, Category: "effect/audit behavior", Preconditions: []string{}, Action: "invoke " + source.Intent.Name, ExpectedOutcome: "observes " + effect.Name, RequiredEffects: []string{effect.Name}, ForbiddenEffects: []string{}, ExpectedFailures: []string{}, SourceClaimIDs: []string{effect.PlanRecordID}, ObligationIDs: []string{effect.PlanRecordID}})
			addCoverage(out, effect.PlanRecordID, CoverageCovered, "", "test-"+effect.PlanRecordID)
		}
		if effect.Forbidden {
			addCase(out, TestCase{ID: "test-forbidden-" + effect.PlanRecordID, Category: "effect/audit behavior", Preconditions: []string{}, Action: "invoke " + source.Intent.Name, ExpectedOutcome: "does not observe " + effect.Name, RequiredEffects: []string{}, ForbiddenEffects: []string{effect.Name}, ExpectedFailures: []string{}, SourceClaimIDs: []string{effect.PlanRecordID}, ObligationIDs: []string{effect.PlanRecordID}})
			addCoverage(out, effect.PlanRecordID, CoverageCovered, "", "test-forbidden-"+effect.PlanRecordID)
		}
	}
	for _, failure := range lowered.Failures {
		if failure.Strategy != "policy" {
			addCase(out, TestCase{ID: "test-" + failure.PlanRecordID, Category: "failure propagation", Preconditions: []string{}, Action: "invoke " + source.Intent.Name, ExpectedOutcome: "reports " + failure.Code, RequiredEffects: []string{}, ForbiddenEffects: []string{}, ExpectedFailures: []string{failure.Code}, SourceClaimIDs: []string{failure.PlanRecordID}, ObligationIDs: []string{}})
			addCoverage(out, failure.PlanRecordID, CoverageCovered, "", "test-"+failure.PlanRecordID)
		}
	}
	for _, obligation := range source.Obligations {
		if obligation.Kind != "audit" && obligation.Kind != "failure.wrap" {
			addCoverage(out, obligation.ID, CoverageNotGeneratable, "No target-independent observable test can be derived.")
			out.Gaps = append(out.Gaps, VerificationGap{PlanRecordID: obligation.ID, Reason: "Static or manual verification required."})
		}
	}
	for _, repairPlan := range repairs {
		for _, change := range repairPlan.Changes {
			if change.Kind != "recipe_patch" {
				continue
			}
			caseID := "test-regression-" + repairPlan.ID + "-" + change.TargetID
			addCase(out, TestCase{ID: caseID, Category: "regression", Preconditions: []string{"reproduce repair finding " + repairPlan.ID}, Action: "invoke " + source.Intent.Name, ExpectedOutcome: "prevents recurrence of " + change.Requirement, RequiredEffects: []string{}, ForbiddenEffects: []string{}, ExpectedFailures: []string{}, SourceClaimIDs: []string{change.TargetID}, ObligationIDs: []string{change.TargetID}})
			addCoverage(out, change.TargetID, CoverageCovered, "", caseID)
		}
	}
	sort.Slice(out.Cases, func(i, j int) bool { return out.Cases[i].ID < out.Cases[j].ID })
	sort.Slice(out.Coverage, func(i, j int) bool { return out.Coverage[i].PlanRecordID < out.Coverage[j].PlanRecordID })
	hashInput := source.ID + "\x00" + lowered.ID
	for _, repairPlan := range repairs {
		hashInput += "\x00" + repairPlan.ID
	}
	sum := sha256.Sum256([]byte(hashInput))
	out.ID = "testplan-" + hex.EncodeToString(sum[:16])
	return out, nil
}

func addCase(out *Plan, testCase TestCase) {
	for _, existing := range out.Cases {
		if existing.ID == testCase.ID {
			return
		}
	}
	out.Cases = append(out.Cases, testCase)
}

func addCoverage(out *Plan, recordID string, status CoverageStatus, explanation string, testCaseIDs ...string) {
	for index := range out.Coverage {
		if out.Coverage[index].PlanRecordID != recordID {
			continue
		}
		out.Coverage[index].TestCaseIDs = append(out.Coverage[index].TestCaseIDs, testCaseIDs...)
		sort.Strings(out.Coverage[index].TestCaseIDs)
		if status == CoverageCovered {
			out.Coverage[index].Status = status
			out.Coverage[index].Emitted = EvidenceYes
		}
		return
	}
	emitted := EvidenceNo
	if status == CoverageCovered {
		emitted = EvidenceYes
	}
	out.Coverage = append(out.Coverage, CoverageExpectation{PlanRecordID: recordID, Status: status, TestCaseIDs: testCaseIDs, Emitted: emitted, Executable: EvidenceUnknown, Execution: ExecutionNotRun, SemanticallyCovered: EvidenceUnknown, Explanation: explanation})
}
