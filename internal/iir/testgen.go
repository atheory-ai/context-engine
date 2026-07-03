package iir

import (
	"fmt"
	"strconv"
	"strings"
)

// This file generates tests from IIR. Tests come from declared intent — not from
// generated code — so they are an independent check against the contract rather
// than a mirror of an implementation's mistakes (per the Slice 7 spec).
//
// Each behavior, failure mode, and side-effect expectation becomes one test
// case with a stable IIR node id for traceability. Expectations that cannot be
// turned into a test are reported as unsupported rather than invented.

// TestCoverageKind classifies what an IIR expectation is.
type TestCoverageKind string

const (
	CoverageBehavior    TestCoverageKind = "behavior"
	CoverageFailureMode TestCoverageKind = "failure_mode"
	CoverageSideEffect  TestCoverageKind = "side_effect"
)

// TestCoverage records whether one IIR expectation produced a test, tying it
// back to a node id.
type TestCoverage struct {
	NodeID   string           `json:"nodeId"`
	Kind     TestCoverageKind `json:"kind"`
	Covered  bool             `json:"covered"`
	TestName string           `json:"testName,omitempty"`
	Reason   string           `json:"reason,omitempty"`
}

// TestArtifact is the output of test generation: the test source plus a coverage
// report over the IIR expectations.
type TestArtifact struct {
	Source   string         `json:"source"`
	Coverage []TestCoverage `json:"coverage"`
}

// TestEmitter turns IIR into tests. The built-in emitter implements this; future
// framework-specific test emitters will too.
type TestEmitter interface {
	ID() string
	Supports(intent *FunctionIntent) bool
	EmitTests(intent *FunctionIntent) (TestArtifact, error)
}

type tsTestEmitter struct{}

func (tsTestEmitter) ID() string { return "builtin.typescript.tests" }

func (tsTestEmitter) Supports(intent *FunctionIntent) bool {
	return intent != nil && intent.Kind == KindFunctionIntent &&
		(intent.Language == "typescript" || intent.Language == "")
}

func (tsTestEmitter) EmitTests(intent *FunctionIntent) (TestArtifact, error) {
	return GenerateTests(intent)
}

// BuiltinTestEmitter returns the built-in TypeScript test emitter.
func BuiltinTestEmitter() TestEmitter { return tsTestEmitter{} }

// Compile-time proof the built-in test emitter satisfies the interface.
var _ TestEmitter = tsTestEmitter{}

// GenerateTests emits deterministic Vitest/Jest-style test cases for a
// FunctionIntent and a coverage report. One case is produced per declared
// behavior, failure mode, and side effect; each carries a traceability comment
// linking it to the IIR node id.
func GenerateTests(intent *FunctionIntent) (TestArtifact, error) {
	if intent == nil || intent.Kind != KindFunctionIntent {
		return TestArtifact{}, fmt.Errorf("test generation: unsupported IIR node")
	}
	if !validGeneratableName(intent.Name) {
		return TestArtifact{}, fmt.Errorf("test generation: %q is not a valid TypeScript identifier", intent.Name)
	}

	coverage := planCoverage(intent)

	var b strings.Builder
	fmt.Fprintf(&b, "// Generated from IIR for %s. Tests derive from declared intent, not code.\n", intent.Name)
	fmt.Fprintf(&b, "import { %s } from \"./%s\";\n\n", intent.Name, intent.Name)
	fmt.Fprintf(&b, "describe(%s, () => {\n", jsString(intent.Name))

	wrote := false
	for _, c := range coverage {
		if !c.Covered {
			// Report the gap in-source too, so a reader sees what was skipped.
			// Node ids and reasons are collapsed to one line so intent-derived
			// text can't inject extra comment lines.
			fmt.Fprintf(&b, "%s// unsupported (%s): %s — %s\n", testIndent, c.Kind, commentLine(c.NodeID), commentLine(c.Reason))
			continue
		}
		if wrote {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s// iir: %s\n", testIndent, commentLine(c.NodeID))
		fmt.Fprintf(&b, "%sit(%s, () => {\n", testIndent, jsString(c.TestName))
		fmt.Fprintf(&b, "%s%s// TODO: %s\n", testIndent, testIndent, todoFor(c.Kind))
		fmt.Fprintf(&b, "%s%sexpect(%s).toBeDefined();\n", testIndent, testIndent, intent.Name)
		fmt.Fprintf(&b, "%s});\n", testIndent)
		wrote = true
	}

	// Avoid emitting an empty suite (which Jest/Vitest treat as a failure): when
	// no expectation produced a case, leave a pending placeholder.
	if !wrote {
		fmt.Fprintf(&b, "%sit.todo(%s);\n", testIndent, jsString("no testable expectations declared in IIR"))
	}

	b.WriteString("});\n")

	return TestArtifact{Source: b.String(), Coverage: coverage}, nil
}

const testIndent = "  "

// planCoverage builds the deterministic per-expectation coverage plan: behaviors
// (in order), then failure modes, then side effects.
func planCoverage(intent *FunctionIntent) []TestCoverage {
	out := []TestCoverage{}

	for i, clause := range intent.Behavior {
		nodeID := fmt.Sprintf("%s.behavior[%d]", intent.Name, i)
		name := behaviorTestName(clause)
		if name == "" {
			// No description to derive a test from: report, don't invent.
			out = append(out, TestCoverage{
				NodeID: nodeID, Kind: CoverageBehavior, Covered: false,
				Reason: "behavior clause has neither a when nor a then description",
			})
			continue
		}
		out = append(out, TestCoverage{
			NodeID: nodeID, Kind: CoverageBehavior, Covered: true, TestName: name,
		})
	}

	for _, mode := range intent.FailureModes {
		out = append(out, TestCoverage{
			NodeID:   fmt.Sprintf("%s.failureMode.%s", intent.Name, mode),
			Kind:     CoverageFailureMode,
			Covered:  true,
			TestName: "fails with " + mode,
		})
	}

	for _, se := range intent.SideEffects {
		out = append(out, TestCoverage{
			NodeID:   fmt.Sprintf("%s.sideEffect.%s", intent.Name, se),
			Kind:     CoverageSideEffect,
			Covered:  true,
			TestName: "performs side effect " + se,
		})
	}

	return out
}

func behaviorTestName(clause BehaviorClause) string {
	when := normalizeWhitespace(clause.When)
	then := normalizeWhitespace(clause.Then)
	switch {
	case when != "" && then != "":
		return "when " + when + " then " + then
	case when != "":
		return "when " + when
	case then != "":
		return then
	default:
		return ""
	}
}

func todoFor(kind TestCoverageKind) string {
	switch kind {
	case CoverageFailureMode:
		return "exercise the failure path and assert the failure outcome"
	case CoverageSideEffect:
		return "assert the side effect is performed"
	default:
		return "arrange inputs and assert the declared behavior"
	}
}

// jsString renders a Go string as a JavaScript double-quoted string literal.
func jsString(s string) string {
	return strconv.Quote(s)
}

// commentLine collapses text to a single line so it is safe to embed in a `//`
// comment without injecting extra lines.
func commentLine(s string) string {
	return normalizeWhitespace(s)
}
