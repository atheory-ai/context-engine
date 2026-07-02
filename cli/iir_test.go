package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
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
