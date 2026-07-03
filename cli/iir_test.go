package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testIntentYAML = `
kind: FunctionIntent
name: validateDonationAmount
language: typescript
inputs:
  - name: amount
    type: Money
  - name: campaign
    type: Campaign
returns:
  type: ValidationResult<Money>
sideEffects: []
`

const testCleanSource = `
import { ok } from "./result";
export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> {
  return ok(amount);
}
`

const testDirtySource = `
import { analytics } from "./analytics";
import { ok } from "./result";
export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> {
  analytics.track("validated");
  return ok(amount);
}
`

// runVerify executes the verify subcommand in isolation and returns the RunE
// error, exercising the report.Status → exit-code contract.
func runVerify(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newIirVerifyCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func runGenerate(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newIirGenerateCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestIirGenerate_RoundTripVerifyPasses(t *testing.T) {
	intent := writeTemp(t, "intent.yaml", testIntentYAML)
	// Isolate cwd so --verify's rule discovery can't pick up a project rule
	// pack that happens to live above the real working directory.
	t.Chdir(t.TempDir())
	if err := runGenerate(t, intent, "--verify"); err != nil {
		t.Errorf("expected generated source to round-trip, got %v", err)
	}
}

func TestIirGenerate_NoVerifyJustEmits(t *testing.T) {
	intent := writeTemp(t, "intent.yaml", testIntentYAML)
	if err := runGenerate(t, intent); err != nil {
		t.Errorf("expected plain generate to succeed, got %v", err)
	}
}

func runGenTests(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newIirGenTestsCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestIirGenTests_EmitsAndReportsCoverage(t *testing.T) {
	intent := writeTemp(t, "intent.yaml", testIntentYAML)

	cmd := newIirGenTestsCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{intent, "--coverage"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected gen-tests to succeed, got %v", err)
	}

	// stdout carries the test source; stderr carries the coverage report.
	if !strings.Contains(out.String(), "describe(\"validateDonationAmount\"") {
		t.Errorf("expected a describe block, got:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "coverage:") {
		t.Errorf("expected a coverage summary, got:\n%s", errOut.String())
	}
}

func TestIirGenTests_MissingIntentFileIsLoudError(t *testing.T) {
	err := runGenTests(t, filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || errors.Is(err, errSilent) {
		t.Errorf("expected a loud error for a missing intent file, got %v", err)
	}
}

func TestIirGenerate_MissingIntentFileIsLoudError(t *testing.T) {
	err := runGenerate(t, filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || errors.Is(err, errSilent) {
		t.Errorf("expected a loud error for a missing intent file, got %v", err)
	}
}

func TestIirGenerate_InvalidRulesPathIsLoudError(t *testing.T) {
	intent := writeTemp(t, "intent.yaml", testIntentYAML)
	err := runGenerate(t, intent, "--verify", "--rules", filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil || errors.Is(err, errSilent) {
		t.Errorf("expected a loud error for a missing --rules file, got %v", err)
	}
}

func TestIirVerify_PassExitsZero(t *testing.T) {
	intent := writeTemp(t, "intent.yaml", testIntentYAML)
	src := writeTemp(t, "clean.ts", testCleanSource)
	if err := runVerify(t, intent, src, "--json"); err != nil {
		t.Errorf("expected nil error (exit 0) for passing verify, got %v", err)
	}
}

func TestIirVerify_FailReturnsSilentError(t *testing.T) {
	intent := writeTemp(t, "intent.yaml", testIntentYAML)
	src := writeTemp(t, "dirty.ts", testDirtySource)
	err := runVerify(t, intent, src, "--json")
	if err == nil {
		t.Fatal("expected non-nil error (non-zero exit) for failing verify")
	}
	// The failure is reported via the report itself, so the exit-code signal is
	// the silent sentinel rather than a user-facing error message.
	if !errors.Is(err, errSilent) {
		t.Errorf("expected errSilent, got %v", err)
	}
}

func TestIirVerify_MissingIntentFileIsLoudError(t *testing.T) {
	src := writeTemp(t, "clean.ts", testCleanSource)
	err := runVerify(t, filepath.Join(t.TempDir(), "nope.yaml"), src)
	if err == nil || errors.Is(err, errSilent) {
		t.Errorf("expected a loud (non-silent) error for missing intent file, got %v", err)
	}
}

// A function that declares failure modes but returns them (not via a Result
// type) triggers only a warning under the default pack, so verify passes.
const testFailureModeIntent = `
kind: FunctionIntent
name: f
language: typescript
returns:
  type: void
failureModes:
  - bad_input
sideEffects: []
`

const testThrowSource = `export function f(): void { throw new Error("bad_input"); }`

// A project rule pack that promotes the failure-strategy rule to an error.
const testProjectPack = `
rules:
  - id: expected-failures-use-result
    target: FunctionIntent
    severity: error
    when:
      hasFailureModes: true
    require:
      failureStrategy: ResultType
`

func TestIirVerify_DiscoversAndLayersProjectRulePack(t *testing.T) {
	dir := t.TempDir()
	intent := filepath.Join(dir, "intent.yaml")
	src := filepath.Join(dir, "throws.ts")
	if err := os.WriteFile(intent, []byte(testFailureModeIntent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(testThrowSource), 0o644); err != nil {
		t.Fatal(err)
	}
	// Run with the temp dir as cwd so auto-discovery searches it.
	t.Chdir(dir)

	// Without a project pack, the built-in failure-strategy rule is a warning →
	// verification passes.
	if err := runVerify(t, intent, src); err != nil {
		t.Fatalf("expected pass with default rules, got %v", err)
	}

	// Drop a project pack that promotes the rule to an error; discovery must
	// pick it up and layer it, flipping the outcome to a failure.
	if err := os.WriteFile(filepath.Join(dir, "iir.rules.yaml"), []byte(testProjectPack), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runVerify(t, intent, src)
	if !errors.Is(err, errSilent) {
		t.Fatalf("expected failure after project pack promotes the rule, got %v", err)
	}
}
