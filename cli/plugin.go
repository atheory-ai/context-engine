package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/plugins/runtime"
	"github.com/spf13/cobra"
)

func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage CE plugins",
	}

	// validate
	validateCmd := &cobra.Command{
		Use:   "validate <file.wasm>",
		Short: "Validate a .wasm plugin file",
		Args:  cobra.ExactArgs(1),
		RunE:  runPluginValidate,
	}
	validateCmd.Flags().Bool("json", false, "output machine-readable JSON")

	// extract — internal command used by ce-sandbox
	extractCmd := &cobra.Command{
		Use:    "extract <file.wasm>",
		Short:  "Run language extraction on a fixture file",
		Long:   "Internal command used by ce-sandbox. Loads a plugin and calls ce_language_extract.",
		Args:   cobra.ExactArgs(1),
		RunE:   runPluginExtract,
		Hidden: true,
	}
	extractCmd.Flags().String("input", "", "path to JSON file with {filePath, content}")
	_ = extractCmd.MarkFlagRequired("input")

	// list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		RunE:  runPluginList,
	}

	// build (stub — requires Javy toolchain)
	buildCmd := &cobra.Command{
		Use:   "build [path]",
		Short: "Compile TypeScript/JS plugin to .wasm",
		RunE:  pluginStub,
	}
	buildCmd.Flags().String("output", "", "output .wasm file path")

	// dev (stub)
	devCmd := &cobra.Command{
		Use:   "dev [path]",
		Short: "Live development loop with coverage analysis",
		RunE:  pluginStub,
	}
	devCmd.Flags().Bool("watch", true, "watch for file changes")

	cmd.AddCommand(
		validateCmd,
		extractCmd,
		listCmd,
		buildCmd,
		devCmd,
		&cobra.Command{
			Use:   "install <file>",
			Short: "Register a plugin in ce.yaml",
			Args:  cobra.ExactArgs(1),
			RunE:  pluginStub,
		},
		&cobra.Command{
			Use:   "remove <name>",
			Short: "Unregister a plugin",
			Args:  cobra.ExactArgs(1),
			RunE:  pluginStub,
		},
	)

	return cmd
}

// ── validate ──────────────────────────────────────────────────────────────────

type pluginValidateResult struct {
	Passed   bool     `json:"passed"`
	File     string   `json:"file"`
	PluginID string   `json:"plugin_id,omitempty"`
	Name     string   `json:"name,omitempty"`
	Version  string   `json:"version,omitempty"`
	Errors   []string `json:"errors,omitempty"`

	Capabilities pluginCapabilitiesJSON `json:"capabilities,omitempty"`
}

type pluginCapabilitiesJSON struct {
	Language  bool     `json:"language"`
	Role      bool     `json:"role"`
	Analyzers []string `json:"analyzers,omitempty"`
	Tools     []string `json:"tools,omitempty"`
}

func runPluginValidate(cmd *cobra.Command, args []string) error {
	wasmPath := args[0]
	jsonOut, _ := cmd.Flags().GetBool("json")

	result := validateWASMPlugin(cmd.Context(), wasmPath)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if result.Passed {
		fmt.Printf("Plugin:  %s v%s\n", result.Name, result.Version)
		fmt.Printf("ID:      %s\n", result.PluginID)
		fmt.Printf("Status:  OK\n")
		fmt.Printf("\nCapabilities:\n")
		fmt.Printf("  Language handler:  %v\n", result.Capabilities.Language)
		fmt.Printf("  Agent role:        %v\n", result.Capabilities.Role)
		if len(result.Capabilities.Tools) > 0 {
			fmt.Printf("  Tools:             %v\n", result.Capabilities.Tools)
		}
		if len(result.Capabilities.Analyzers) > 0 {
			fmt.Printf("  Analyzers:         %v\n", result.Capabilities.Analyzers)
		}
		return nil
	}

	fmt.Fprintf(os.Stderr, "Status:  INVALID\n")
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "  error: %s\n", e)
	}
	return fmt.Errorf("plugin validation failed")
}

func validateWASMPlugin(ctx context.Context, wasmPath string) pluginValidateResult {
	result := pluginValidateResult{File: wasmPath}

	// Use a temp dir for the wazero compilation cache — validate is stateless.
	tmpDir, err := os.MkdirTemp("", "ce-validate-*")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("create temp dir: %v", err))
		return result
	}
	defer os.RemoveAll(tmpDir)

	ch := core.NewAppChannels()
	rt, err := runtime.New(tmpDir, &ch)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("init plugin runtime: %v", err))
		return result
	}
	defer rt.Close()

	plugin, err := rt.Load(ctx, wasmPath, nil)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	defer plugin.Close()

	result.Passed = true
	result.PluginID = string(plugin.ID())
	result.Name = plugin.Name()
	result.Version = plugin.Version()

	// Introspect capabilities
	caps := pluginCapabilitiesJSON{
		Language: plugin.Language() != nil,
		Role:     len(plugin.Roles()) > 0,
	}
	for _, t := range plugin.Tools() {
		caps.Tools = append(caps.Tools, t.Name())
	}
	for _, a := range plugin.Analyzers() {
		caps.Analyzers = append(caps.Analyzers, a.Name())
	}
	result.Capabilities = caps
	return result
}

// ── extract ───────────────────────────────────────────────────────────────────

// extractInput matches the JSON body the ce-sandbox writes.
type extractInput struct {
	FilePath string `json:"filePath"`
	Content  string `json:"content"`
}

func runPluginExtract(cmd *cobra.Command, args []string) error {
	wasmPath := args[0]
	inputPath, _ := cmd.Flags().GetString("input")

	inputBytes, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	var inp extractInput
	if err := json.Unmarshal(inputBytes, &inp); err != nil {
		return fmt.Errorf("parse input JSON: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "ce-extract-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	ch := core.NewAppChannels()
	rt, err := runtime.New(tmpDir, &ch)
	if err != nil {
		return fmt.Errorf("init plugin runtime: %w", err)
	}
	defer rt.Close()

	plugin, err := rt.Load(cmd.Context(), wasmPath, nil)
	if err != nil {
		return fmt.Errorf("load plugin: %w", err)
	}
	defer plugin.Close()

	lang := plugin.Language()
	if lang == nil {
		return fmt.Errorf("plugin %s does not declare a language handler", plugin.Name())
	}

	extraction, err := lang.Extract(inp.FilePath, []byte(inp.Content), nil)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(toExtractionJSON(extraction))
}

// toExtractionJSON converts core.ExtractionResult to the JSON shape
// the TypeScript @ce/plugin-sdk types describe (camelCase, matching ExtractionResult).
func toExtractionJSON(r core.ExtractionResult) any {
	type nodeJSON struct {
		ID          string         `json:"id"`
		Type        string         `json:"type"`
		Label       string         `json:"label"`
		CanonicalID string         `json:"canonicalID"`
		SourceClass string         `json:"sourceClass"`
		Properties  map[string]any `json:"properties"`
	}
	type edgeJSON struct {
		ID          string         `json:"id"`
		SourceID    string         `json:"sourceID"`
		TargetID    string         `json:"targetID"`
		Type        string         `json:"type"`
		SourceClass string         `json:"sourceClass"`
		Properties  map[string]any `json:"properties"`
	}
	type resultJSON struct {
		Nodes []nodeJSON `json:"nodes"`
		Edges []edgeJSON `json:"edges"`
	}

	nodes := make([]nodeJSON, len(r.Nodes))
	for i, n := range r.Nodes {
		props := n.Properties
		if props == nil {
			props = map[string]any{}
		}
		nodes[i] = nodeJSON{
			ID:          string(n.ID),
			Type:        n.Type,
			Label:       n.Label,
			CanonicalID: n.CanonicalID,
			SourceClass: string(n.SourceClass),
			Properties:  props,
		}
	}

	edges := make([]edgeJSON, len(r.Edges))
	for i, e := range r.Edges {
		props := e.Properties
		if props == nil {
			props = map[string]any{}
		}
		edges[i] = edgeJSON{
			ID:          string(e.ID),
			SourceID:    string(e.SourceID),
			TargetID:    string(e.TargetID),
			Type:        e.Type,
			SourceClass: string(e.SourceClass),
			Properties:  props,
		}
	}

	return resultJSON{Nodes: nodes, Edges: edges}
}

// ── list ──────────────────────────────────────────────────────────────────────

func runPluginList(_ *cobra.Command, _ []string) error {
	cfg, err := config.LoadRaw()
	if err != nil {
		return err
	}

	if len(cfg.Plugins) == 0 {
		fmt.Println("No plugins installed.")
		fmt.Println("Add entries to ce.yaml under the 'plugins:' key.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tSTATUS")
	for _, p := range cfg.Plugins {
		status := "ok"
		if _, err := os.Stat(p.Path); os.IsNotExist(err) {
			status = "missing (file not found)"
		}
		fmt.Fprintf(w, "%s\t%s\n", p.Path, status)
	}
	return w.Flush()
}

// ── stubs ─────────────────────────────────────────────────────────────────────

func pluginStub(_ *cobra.Command, _ []string) error {
	fmt.Println("Not yet implemented.")
	return nil
}
