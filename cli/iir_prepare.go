package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/semantic/decorate"
	"github.com/atheory-ai/context-engine/internal/semantic/packet"
	"github.com/atheory-ai/context-engine/internal/semantic/passes"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/shaping"
	"github.com/atheory-ai/context-engine/internal/semantic/vocabulary"
	"github.com/spf13/cobra"
)

// preparationOutput is intentionally source-free. It is the immutable,
// evidence-backed input an implementation LLM or harness agent consumes.
type preparationOutput struct {
	Plan     *plan.SemanticPlan           `json:"plan"`
	Packet   *packet.ImplementationPacket `json:"packet"`
	Findings []passes.Finding             `json:"findings"`
	Plugins  []string                     `json:"appliedPluginIds"`
	Skipped  []string                     `json:"skippedPluginIds"`
	Executor string                       `json:"executor"`
}

func newIirPrepareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prepare [description]",
		Short: "Compile a decorated implementation contract for an LLM or harness agent",
		Long: `Shape natural language into a candidate IIR, then deterministically apply
the semantic policies declared by the active plugins. The result is an
evidence-backed implementation packet; CE does not generate or write source.

By default a description uses the configured direct LLM provider. --intent
accepts a declared IIR file and requires no provider. The packet stays blocked
when the target or another mandatory decision is unresolved.`,
		RunE: runIirPrepare,
	}
	cmd.Flags().String("intent", "", "declared FunctionIntent YAML/JSON file (avoids model shaping)")
	cmd.Flags().String("target", "", "canonical target symbol or requested unit ID")
	cmd.Flags().String("language", "", "target language; defaults to the intent language")
	cmd.Flags().StringArray("context", nil, "declared context tag used for policy selection (repeatable, e.g. woocommerce.checkout)")
	cmd.Flags().StringArray("tag", nil, "declared controlled semantic tag used for policy selection (repeatable, e.g. operation.cart.modify)")
	cmd.Flags().Bool("prompt", false, "print the implementation-agent prompt instead of JSON")
	return cmd
}

func runIirPrepare(cmd *cobra.Command, args []string) error {
	intentPath, _ := cmd.Flags().GetString("intent")
	target, _ := cmd.Flags().GetString("target")
	language, _ := cmd.Flags().GetString("language")
	printPrompt, _ := cmd.Flags().GetBool("prompt")
	contexts, _ := cmd.Flags().GetStringArray("context")
	tags, _ := cmd.Flags().GetStringArray("tag")
	description := strings.TrimSpace(strings.Join(args, " "))
	if intentPath == "" && description == "" {
		return fmt.Errorf("provide a description or --intent")
	}
	if intentPath != "" && description != "" {
		return fmt.Errorf("provide a description or --intent, not both")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	prepared, err := prepareImplementationContract(cmd.Context(), cfg, intentPath, description, target, language, contexts, tags)
	if err != nil {
		return err
	}
	if printPrompt {
		prompt, err := packet.Prompt(prepared.Packet)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), prompt)
		return err
	}
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(prepared)
}

func prepareImplementationContract(ctx context.Context, cfg *config.Config, intentPath, description, target, language string, contexts, tags []string) (*preparationOutput, error) {
	semanticPlan, executor, err := prepareInitialPlan(ctx, cfg, intentPath, description, target, language)
	if err != nil {
		return nil, err
	}
	semanticPlan, err = addDeclaredTags(semanticPlan, contexts, tags)
	if err != nil {
		return nil, err
	}
	ch := core.NewAppChannels()
	contributions, cleanup := runner.PluginSemanticPolicies(ctx, cfg, &ch)
	defer cleanup()
	if err := warningsAsError(&ch); err != nil {
		return nil, err
	}
	pluginInput := make([]decorate.Contribution, 0, len(contributions))
	for _, contribution := range contributions {
		pluginInput = append(pluginInput, decorate.Contribution{PluginID: string(contribution.PluginID), Version: contribution.Version, Raw: contribution.Raw})
	}
	decorated, err := decorate.Apply(semanticPlan, decorate.Input{Plugins: pluginInput})
	if err != nil {
		return nil, err
	}
	implementationPacket, err := packet.Build(decorated.Plan)
	if err != nil {
		return nil, err
	}
	return &preparationOutput{
		Plan: decorated.Plan, Packet: implementationPacket, Findings: decorated.Findings,
		Plugins: decorated.AppliedPluginIDs, Skipped: decorated.SkippedPluginIDs, Executor: executor,
	}, nil
}

// addDeclaredContext is a temporary explicit input seam for a caller that
// already knows the framework context. Graph-backed resolution will produce the
// same claims as observed evidence; this path marks them declared and makes the
// distinction visible in the packet and policy history.
func addDeclaredContext(source *plan.SemanticPlan, contexts []string) (*plan.SemanticPlan, error) {
	return addDeclaredTags(source, contexts, nil)
}

// addDeclaredTags is a temporary caller-input seam. Both legacy --context and
// generic --tag values are checked against the same controlled vocabulary;
// graph-backed resolution will eventually emit these claims as observed facts.
func addDeclaredTags(source *plan.SemanticPlan, contexts, tags []string) (*plan.SemanticPlan, error) {
	values := make([]string, 0, len(contexts)+len(tags))
	for _, context := range contexts {
		context = strings.TrimSpace(context)
		if context == "" {
			continue
		}
		values = append(values, "context."+context)
	}
	values = append(values, tags...)
	values, err := vocabulary.Normalize(values)
	if err != nil {
		return nil, fmt.Errorf("declared semantic tags: %w", err)
	}
	if len(values) == 0 {
		return source, nil
	}
	candidate := *source
	candidate.Claims = append([]plan.SemanticClaim{}, source.Claims...)
	candidate.PassRecords = append([]plan.PassRecord{}, source.PassRecords...)
	for _, tag := range values {
		claimID := plan.StableRecordID("claim", "declared", tag)
		candidate.Claims = append(candidate.Claims, plan.SemanticClaim{
			ID: claimID, Kind: tag, Statement: tag, State: plan.KnowledgeDeclared,
			Evidence: []plan.Evidence{{ID: claimID + ".evidence", Source: "user", Producer: "cli.iir.prepare", Confidence: plan.ConfidenceHigh, Explanation: "Caller-declared controlled semantic tag."}},
		})
	}
	candidate.PassRecords = append(candidate.PassRecords, plan.PassRecord{
		ID: plan.StableRecordID("pass", source.ID, "declared-context"), PassID: "semantic.context.declared", Version: "v1", Phase: "resolve", Inputs: []string{source.ID}, Outputs: values,
		Evidence: []plan.Evidence{{ID: plan.StableRecordID("evidence", source.ID, "declared-context"), Source: "user", Producer: "cli.iir.prepare", Confidence: plan.ConfidenceHigh, Explanation: "Explicit context tags supplied to semantic preparation."}},
	})
	return plan.NewRevision(source, &candidate)
}

func prepareInitialPlan(ctx context.Context, cfg *config.Config, intentPath, description, target, language string) (*plan.SemanticPlan, string, error) {
	unit := plan.SemanticUnit{Scope: "function", Language: language, SourceRefs: []plan.SourceRef{}}
	if target != "" {
		unit.ID = plan.StableRecordID("unit", target)
		unit.CanonicalID = target
	}
	input := shaping.Input{ProjectID: "local", Unit: unit}
	if intentPath != "" {
		intent, err := iir.LoadIntentFile(intentPath)
		if err != nil {
			return nil, "", err
		}
		input.Intent = intent
		semanticPlan, err := shaping.NewWithShaper(nil).FromIntent(input)
		return semanticPlan, "declared", err
	}
	input.Description = description
	provider := runner.NewLLMProvider(cfg)
	if provider == nil {
		return nil, "", fmt.Errorf("no direct intent-shaping provider is configured; supply --intent or run through a harness executor")
	}
	semanticPlan, err := shaping.New(provider).Shape(ctx, input)
	return semanticPlan, "direct", err
}

func warningsAsError(ch *core.AppChannels) error {
	var messages []string
	for {
		select {
		case emission := <-ch.Warning:
			messages = append(messages, emission.Content)
		case emission := <-ch.Error:
			messages = append(messages, emission.Content)
		default:
			if len(messages) == 0 {
				return nil
			}
			return fmt.Errorf("load semantic plugin policies: %s", strings.Join(messages, "; "))
		}
	}
}
