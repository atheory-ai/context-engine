package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/iir/shaper"
	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/semantic/lift"
	"github.com/atheory-ai/context-engine/internal/semantic/mutation"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
	"github.com/spf13/cobra"
)

func newIirImplementCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "implement [description]",
		Short: "Experimental: lower a domain mutation through the semantic pipeline",
		Long: `Compile a mutation description or declared IIR into a semantic plan,
implementation recipe, source candidate, and observed-semantic report.

The default is read-only: it never writes source or the substrate. To write the
displayed source candidate, specify both --write and --out explicitly.`,
		RunE: runIirImplement,
	}
	cmd.Flags().String("intent", "", "declared FunctionIntent YAML/JSON file (avoids model shaping)")
	cmd.Flags().Bool("write", false, "write the source candidate (requires --out)")
	cmd.Flags().String("out", "", "explicit output path used only with --write")
	return cmd
}

func runIirImplement(cmd *cobra.Command, args []string) error {
	intentPath, _ := cmd.Flags().GetString("intent")
	write, _ := cmd.Flags().GetBool("write")
	outPath, _ := cmd.Flags().GetString("out")
	if write && outPath == "" {
		return fmt.Errorf("--write requires --out")
	}
	if intentPath == "" && len(args) == 0 {
		return fmt.Errorf("provide a description or --intent")
	}
	intent, err := implementIntent(cmd.Context(), intentPath, strings.Join(args, " "))
	if err != nil {
		return err
	}
	semanticPlan, err := implementPlan(intent)
	if err != nil {
		return err
	}
	extractor, cleanup, err := iirExtractor(cmd.Context())
	if err != nil {
		return err
	}
	defer cleanup()
	workflow := mutation.Workflow{Renderer: recipe.TypeScriptEmitter{}, Observer: cliObserver{extractor: extractor}, Rules: iir.DefaultRulePack(), Profile: recipe.DefaultProfile("typescript"), Policies: mutation.MutationPolicies()}
	result, err := workflow.Execute(cmd.Context(), semanticPlan)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return err
	}
	if !write {
		return nil
	}
	if result.Source == "" {
		return fmt.Errorf("no source candidate available to write")
	}
	if err := os.WriteFile(outPath, []byte(result.Source), 0o644); err != nil {
		return fmt.Errorf("write source candidate: %w", err)
	}
	return nil
}

func implementIntent(ctx context.Context, path, description string) (*iir.FunctionIntent, error) {
	if path != "" {
		return iir.LoadIntentFile(path)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return shaper.New(runner.NewLLMProvider(cfg)).Shape(ctx, description)
}

func implementPlan(intent *iir.FunctionIntent) (*plan.SemanticPlan, error) {
	semanticPlan, err := plan.NewPlan("cli", plan.SemanticUnit{ID: "requested-" + intent.Name, CanonicalID: "requested." + intent.Name, Scope: "function", Language: intent.Language, SourceRefs: []plan.SourceRef{}}, intent)
	if err != nil {
		return nil, err
	}
	semanticPlan.Lifecycle = plan.LifecycleResolved
	semanticPlan.Claims = []plan.SemanticClaim{{ID: "mutation", Kind: "effect.mutation", Statement: "requested domain mutation", State: plan.KnowledgeDeclared, Evidence: []plan.Evidence{{ID: "mutation-request", Source: "user", Producer: "cli.iir.implement", Explanation: "The implement command is limited to domain mutations."}}}}
	for index, failure := range intent.FailureModes {
		semanticPlan.Claims = append(semanticPlan.Claims, plan.SemanticClaim{ID: fmt.Sprintf("failure-%d", index), Kind: "failure." + failure.Kind, Statement: failure.Code, State: plan.KnowledgeDeclared, Evidence: []plan.Evidence{{ID: fmt.Sprintf("failure-evidence-%d", index), Source: "user", Producer: "cli.iir.implement", Explanation: "Declared failure requirement."}}})
	}
	return semanticPlan, nil
}

// cliObserver adapts the shared plugin extractor to the vertical workflow.
// Current plugins that do not emit the v1 coverage envelope are intentionally
// partial; their source candidate is shown but cannot be reported accepted.
type cliObserver struct{ extractor iir.Extractor }

func (o cliObserver) Observe(ctx context.Context, lowered *recipe.ImplementationRecipe, source string) (*lift.Unit, error) {
	result, err := o.extractor.Extract(ctx, iir.ExtractionInput{Language: lowered.TargetLanguage, Source: []byte(source), Target: lowered.Signature.Name})
	if err != nil {
		return nil, err
	}
	return &lift.Unit{NodeID: "cli-candidate", Language: lowered.TargetLanguage, SchemaVersion: lift.SchemaVersionV1, Observed: result.Function, Claims: []lift.Claim{{ID: "lift-coverage", Kind: "unknown", Statement: "Standalone extractor does not yet expose plugin coverage metadata.", Evidence: []lift.Evidence{}}}, Evidence: []lift.Evidence{}, Coverage: lift.CoveragePartial}, nil
}

var _ mutation.Observer = cliObserver{}
var _ = context.Background
