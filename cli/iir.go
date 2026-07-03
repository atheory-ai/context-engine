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

	cmd.AddCommand(newIirVerifyCmd(), newIirGenerateCmd())
	return cmd
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

	out := cmd.OutOrStderr()
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
