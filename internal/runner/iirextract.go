package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/indexer"
	"github.com/atheory-ai/context-engine/internal/indexer/wasmparse"
	"github.com/atheory-ai/context-engine/internal/plugins"
)

// pluginExtractor implements iir.Extractor by parsing source with the pure-Go
// tree-sitter (wasmparse) and running the matching language plugin's lift — the
// same frontend the indexer uses. This is what lets `ce iir verify` share one
// extraction path with indexing instead of a separate host-side extractor.
type pluginExtractor struct {
	reg    *plugins.Registry
	parser *wasmparse.Parser
}

// extByLanguage maps an IIR language tag to a representative file extension so a
// snippet (which has no path) can be routed to the right grammar and plugin.
var extByLanguage = map[string]string{
	"typescript": ".ts",
	"tsx":        ".tsx",
	"javascript": ".js",
	"go":         ".go",
	"python":     ".py",
}

// NewIIRExtractor loads the default language plugins and a pure-Go parser and
// returns an iir.Extractor backed by plugin lift, plus a cleanup func. It is
// standalone (no substrate/DBs) so the CLI verify commands can extract without a
// full engine. Default plugins are embedded, so this works offline.
func NewIIRExtractor(ctx context.Context, cfg *config.Config, ch *core.AppChannels) (iir.Extractor, func(), error) {
	if err := indexer.ExtractDefaults(cfg.DataDir); err != nil {
		return nil, nil, fmt.Errorf("extract default plugins: %w", err)
	}
	reg := plugins.NewRegistry()
	if err := reg.Initialize(cfg.DataDir, ch); err != nil {
		return nil, nil, fmt.Errorf("init plugin runtime: %w", err)
	}
	defaultsDir := filepath.Join(cfg.DataDir, "plugins", "defaults")
	for _, name := range defaultPluginNames {
		path := filepath.Join(defaultsDir, name)
		if _, err := os.Stat(path); err != nil {
			continue // not built/installed — skip
		}
		if err := reg.Load(ctx, path, nil); err != nil {
			ch.Emit(core.Emission{Source: "iir", Channel: core.ChanWarning,
				Content: fmt.Sprintf("default plugin %s: %v", name, err)})
		}
	}
	parser, err := wasmparse.New(ctx, "")
	if err != nil {
		reg.UnloadAll()
		return nil, nil, fmt.Errorf("wasmparse: %w", err)
	}
	cleanup := func() {
		parser.Close(ctx)
		reg.UnloadAll()
	}
	return &pluginExtractor{reg: reg, parser: parser}, cleanup, nil
}

func (p *pluginExtractor) ID() string { return "plugin.lift" }

// Supports reports whether a plugin frontend exists for the input's language.
func (p *pluginExtractor) Supports(input iir.ExtractionInput) bool {
	_, ok := extByLanguage[input.Language]
	return ok
}

// Extract parses the source and returns the target function's lifted intent (or,
// when Target is empty or absent, the first lifted function so a name mismatch
// surfaces through comparison rather than as a hard error).
func (p *pluginExtractor) Extract(ctx context.Context, input iir.ExtractionInput) (iir.ExtractionResult, error) {
	ext, ok := extByLanguage[input.Language]
	if !ok {
		return iir.ExtractionResult{}, fmt.Errorf("no plugin frontend for language %q", input.Language)
	}
	path := "source" + ext
	treeJSON, err := p.parser.ParseFile(ctx, path, input.Source)
	if err != nil {
		return iir.ExtractionResult{}, fmt.Errorf("parse %s: %w", input.Language, err)
	}

	var first *iir.FunctionIntent
	for _, pl := range p.reg.PluginsForFile(path) {
		lang := pl.Language()
		if lang == nil {
			continue
		}
		res, err := lang.Extract(path, input.Source, treeJSON)
		if err != nil {
			return iir.ExtractionResult{}, fmt.Errorf("plugin extract: %w", err)
		}
		for _, e := range res.IIR {
			fi, err := iir.ParseIntentJSON(e.Intent)
			if err != nil {
				continue
			}
			if input.Target != "" && fi.Name == input.Target {
				return iir.ExtractionResult{Function: fi}, nil
			}
			if first == nil {
				first = fi
			}
		}
	}
	if first != nil {
		return iir.ExtractionResult{Function: first}, nil
	}
	return iir.ExtractionResult{}, fmt.Errorf("no plugin lifted a %s function from the source", input.Language)
}
