package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/spf13/cobra"
)

func newIirCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "iir",
		Short: "Intermediate Intent Representation — verify code against declared intent",
		Long: `IIR is a structured representation of what code is intended to do.

The verify command reads intended IIR, parses a source file, extracts the
actual IIR, compares them, applies rules, and prints a verification report.`,
	}

	cmd.AddCommand(newIirVerifyCmd(), newIirGenerateCmd(), newIirGenTestsCmd(), newIirRepairCmd())
	return cmd
}

func newIirRepairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair <intent-file> <source-file>",
		Short: "Iteratively repair source until it matches declared IIR",
		Long: `Run the repair loop: verify the source against the intent and, while it
fails, apply the built-in deterministic repair (regenerate from intent) and
re-verify, up to --max attempts.

The repaired source is printed to stdout; a convergence summary goes to stderr.
Exits non-zero if verification does not converge.`,
		Args: cobra.ExactArgs(2),
		RunE: runIirRepair,
	}

	cmd.Flags().Int("max", 3, "maximum repair attempts")
	cmd.Flags().String("rules", "",
		"path to a rule pack (YAML/JSON) layered over the built-in defaults; "+
			"when omitted, a project iir.rules.yaml is auto-discovered")
	return cmd
}

func runIirRepair(cmd *cobra.Command, args []string) error {
	intentPath, sourcePath := args[0], args[1]
	maxAttempts, _ := cmd.Flags().GetInt("max")
	rulesPath, _ := cmd.Flags().GetString("rules")

	intent, err := iir.LoadIntentFile(intentPath)
	if err != nil {
		return err
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read source file %s: %w", sourcePath, err)
	}
	pack, _, err := resolveRulePack(rulesPath)
	if err != nil {
		return err
	}

	result, err := iir.RepairLoop(context.Background(), intent, string(source), pack,
		iir.RegenerateStage{}, iir.RepairOptions{MaxIterations: maxAttempts})
	if err != nil {
		return err
	}

	fmt.Fprint(cmd.OutOrStdout(), result.FinalSource)

	errOut := cmd.ErrOrStderr()
	verdict := "converged"
	if !result.Converged {
		verdict = "did not converge"
	}
	fmt.Fprintf(errOut, "\n--- repair: %s after %d iteration(s), final status %s ---\n",
		verdict, len(result.Iterations), result.FinalReport.Status)

	if !result.Converged {
		return errSilent
	}
	return nil
}

func newIirGenTestsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen-tests <intent-file>",
		Short: "Generate tests from declared IIR",
		Long: `Generate deterministic test cases from a FunctionIntent.

Tests derive from declared intent — one case per behavior, failure mode, and
side effect — each tied to an IIR node id for traceability. Expectations that
cannot be turned into a test are reported as unsupported, not invented.

With --coverage, a coverage report over the IIR expectations is printed to
stderr.`,
		Args: cobra.ExactArgs(1),
		RunE: runIirGenTests,
	}

	cmd.Flags().Bool("coverage", false, "print a coverage report over the IIR expectations")
	return cmd
}

func runIirGenTests(cmd *cobra.Command, args []string) error {
	intent, err := iir.LoadIntentFile(args[0])
	if err != nil {
		return err
	}

	artifact, err := iir.GenerateTests(intent)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), artifact.Source)

	showCoverage, _ := cmd.Flags().GetBool("coverage")
	if !showCoverage {
		return nil
	}

	out := cmd.ErrOrStderr()
	covered := 0
	for _, c := range artifact.Coverage {
		if c.Covered {
			covered++
		}
	}
	fmt.Fprintf(out, "\n--- coverage: %d/%d expectations ---\n", covered, len(artifact.Coverage))
	for _, c := range artifact.Coverage {
		status := "covered"
		detail := c.TestName
		if !c.Covered {
			status = "unsupported"
			detail = c.Reason
		}
		fmt.Fprintf(out, "  [%s] %s: %s\n", status, c.NodeID, detail)
	}
	return nil
}

func newIirGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate <intent-file>",
		Short: "Generate TypeScript source from declared IIR",
		Long: `Generate deterministic TypeScript source from a FunctionIntent.

Reads intended IIR (YAML or JSON) and emits a source skeleton whose structure
matches the intent. With --verify, the generated source is re-extracted and
verified back against the intent (the round-trip), exiting non-zero if that
verification fails.`,
		Args: cobra.ExactArgs(1),
		RunE: runIirGenerate,
	}

	cmd.Flags().Bool("verify", false, "re-extract and verify the generated source against the intent")
	cmd.Flags().String("rules", "",
		"path to a rule pack (YAML/JSON) layered over the built-in defaults; "+
			"used with --verify")
	return cmd
}

func runIirGenerate(cmd *cobra.Command, args []string) error {
	intentPath := args[0]
	doVerify, _ := cmd.Flags().GetBool("verify")
	rulesPath, _ := cmd.Flags().GetString("rules")

	intent, err := iir.LoadIntentFile(intentPath)
	if err != nil {
		return err
	}

	source, err := iir.GenerateFunction(intent)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), source)

	if !doVerify {
		return nil
	}

	pack, _, err := resolveRulePack(rulesPath)
	if err != nil {
		return err
	}
	report, err := iir.VerifySource(context.Background(), intent, []byte(source), pack)
	if err != nil {
		return err
	}

	out := cmd.ErrOrStderr()
	fmt.Fprintf(out, "\n--- round-trip: %s ---\n", report.Status)
	for _, m := range report.Mismatches {
		if m.Severity == iir.SeverityError {
			fmt.Fprintf(out, "  [%s] %s: %s\n", m.Severity, m.Kind, m.Message)
		}
	}
	if report.Status != iir.StatusPassed {
		return errSilent
	}
	return nil
}

func newIirVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <intent-file> <source-file>",
		Short: "Verify that a source function matches declared IIR",
		Long: `Verify a single function against its intended IIR.

Reads intended IIR (YAML or JSON), parses the TypeScript source, extracts the
actual IIR, compares them, evaluates rules, and prints a verification report.
Exits non-zero when verification fails.`,
		Args: cobra.ExactArgs(2),
		RunE: runIirVerify,
	}

	cmd.Flags().Bool("json", false, "output the verification report as JSON")
	cmd.Flags().String("rules", "",
		"path to a rule pack (YAML/JSON) layered over the built-in defaults; "+
			"when omitted, a project iir.rules.yaml is auto-discovered")
	return cmd
}

func runIirVerify(cmd *cobra.Command, args []string) error {
	intentPath, sourcePath := args[0], args[1]
	asJSON, _ := cmd.Flags().GetBool("json")
	rulesPath, _ := cmd.Flags().GetString("rules")

	intent, err := iir.LoadIntentFile(intentPath)
	if err != nil {
		return err
	}

	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read source file %s: %w", sourcePath, err)
	}

	pack, rulesSource, err := resolveRulePack(rulesPath)
	if err != nil {
		return err
	}

	report, err := iir.VerifySource(context.Background(), intent, source, pack)
	if err != nil {
		return err
	}

	if asJSON {
		if err := printReportJSON(cmd, report); err != nil {
			return err
		}
	} else {
		printReportHuman(cmd, report, rulesSource)
	}

	if report.Status != iir.StatusPassed {
		return errSilent
	}
	return nil
}

// resolveRulePack builds the effective rule pack: the built-in defaults with an
// explicit (--rules) or auto-discovered project rule pack layered on top. The
// returned string labels the rules source for human output.
func resolveRulePack(rulesPath string) (iir.RulePack, string, error) {
	base := iir.DefaultRulePack()

	if rulesPath != "" {
		override, err := iir.LoadRulePackFile(rulesPath)
		if err != nil {
			// LoadRulePackFile already prefixes the path; add the flag context.
			return iir.RulePack{}, "", fmt.Errorf("invalid --rules pack: %w", err)
		}
		return iir.MergeRulePacks(base, override), rulesPath + " (layered on defaults)", nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return iir.RulePack{}, "", fmt.Errorf("resolve working directory: %w", err)
	}
	override, path, found, err := iir.DiscoverProjectRulePack(cwd)
	if err != nil {
		return iir.RulePack{}, "", fmt.Errorf("discover project rule pack: %w", err)
	}
	if found {
		return iir.MergeRulePacks(base, override), path + " (layered on defaults)", nil
	}
	return base, "built-in defaults", nil
}

func printReportJSON(cmd *cobra.Command, report *iir.Report) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return nil
}

func printReportHuman(cmd *cobra.Command, report *iir.Report, rulesSource string) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "IIR verification: %s\n", report.Status)
	fmt.Fprintf(out, "  function: %s\n", report.Intended.Name)
	fmt.Fprintf(out, "  rules: %s\n", rulesSource)

	if len(report.Matches) > 0 {
		fmt.Fprintf(out, "\n  matches (%d):\n", len(report.Matches))
		for _, m := range report.Matches {
			fmt.Fprintf(out, "    ✓ %s\n", m.Message)
		}
	}

	if len(report.Mismatches) > 0 {
		fmt.Fprintf(out, "\n  mismatches (%d):\n", len(report.Mismatches))
		for _, m := range report.Mismatches {
			fmt.Fprintf(out, "    [%s] %s: %s\n", m.Severity, m.Kind, m.Message)
		}
	}

	reported := false
	for _, r := range report.RuleResults {
		if r.Status == iir.RuleSkipped || r.Status == iir.RulePassed {
			continue
		}
		if !reported {
			fmt.Fprintf(out, "\n  rules:\n")
			reported = true
		}
		fmt.Fprintf(out, "    [%s] %s: %s\n", r.Severity, r.ID, r.Message)
	}

	if len(report.RepairTargets) > 0 {
		fmt.Fprintf(out, "\n  repair targets:\n")
		for _, t := range report.RepairTargets {
			fmt.Fprintf(out, "    - %s\n", t)
		}
	}
}
