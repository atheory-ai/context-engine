// Package goldeniir holds a golden test corpus for IIR extraction. It runs
// fixture source files through the REAL index pipeline — the pure-Go WASM
// tree-sitter parser plus the actual language plugin's lift — and compares the
// emitted FunctionIntents against checked-in golden files. This exercises the
// same parse + wasm-lift path that ships, catching bugs that hand-built
// SyntaxNode trees hide.
//
// The plugins are built artifacts; if they aren't present the test skips with a
// pointer to `make bundle-default-plugins`. Run the full corpus with:
//
//	make test-iir-golden        # builds plugins, then runs with real lift
//	go test ./internal/indexer/goldeniir -update-iir-golden   # regenerate goldens
package goldeniir

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/indexer/wasmparse"
	"github.com/atheory-ai/context-engine/internal/plugins/runtime"
)

var updateGolden = flag.Bool("update-iir-golden", false, "regenerate IIR golden files")

// languages maps a fixture subdirectory to the plugin WASM that lifts it.
var languages = []struct{ dir, wasm string }{
	{"typescript", "typescript.wasm"},
	{"go", "go-language.wasm"},
	{"python", "python.wasm"},
}

func TestIIRGolden(t *testing.T) {
	ctx := context.Background()

	parser, err := wasmparse.New(ctx)
	if err != nil {
		t.Fatalf("wasmparse.New: %v", err)
	}
	defer parser.Close(ctx)

	ch := core.NewAppChannels()
	rt, err := runtime.New(t.TempDir(), &ch)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	for _, lg := range languages {
		t.Run(lg.dir, func(t *testing.T) {
			wasmPath := filepath.Join("..", "defaults", lg.wasm)
			if _, err := os.Stat(wasmPath); err != nil {
				t.Skipf("plugin %s not built — run `make bundle-default-plugins`", lg.wasm)
			}
			plugin, err := rt.Load(ctx, wasmPath, map[string]any{"enabled": true})
			if err != nil {
				t.Fatalf("load %s: %v", lg.wasm, err)
			}
			lang := plugin.Language()
			if lang == nil {
				t.Fatalf("%s exposes no language handler", lg.wasm)
			}

			fixtures, _ := filepath.Glob(filepath.Join("testdata", lg.dir, "*"))
			for _, fx := range fixtures {
				if strings.HasSuffix(fx, ".golden.json") {
					continue
				}
				t.Run(filepath.Base(fx), func(t *testing.T) {
					runFixture(t, ctx, parser, lang, fx)
				})
			}
		})
	}
}

func runFixture(t *testing.T, ctx context.Context, parser *wasmparse.Parser, lang core.LanguageHandler, fx string) {
	content, err := os.ReadFile(fx)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	treeJSON, err := parser.ParseFile(ctx, fx, content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if treeJSON == nil {
		t.Fatalf("no grammar for %s", fx)
	}
	res, err := lang.Extract(filepath.Base(fx), content, treeJSON)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Normalize: parse each attached intent, keep only the FunctionIntent (drop
	// the content-hash node id), sort by name for stable output.
	intents := make([]*iir.FunctionIntent, 0, len(res.IIR))
	for _, e := range res.IIR {
		fi, err := iir.ParseIntentJSON(e.Intent)
		if err != nil {
			t.Fatalf("parse intent: %v\n%s", err, e.Intent)
		}
		intents = append(intents, fi)
	}
	sort.SliceStable(intents, func(i, j int) bool { return intents[i].Name < intents[j].Name })

	got, err := json.MarshalIndent(intents, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')

	goldenPath := fx + ".golden.json"
	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update-iir-golden to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("IIR for %s does not match golden.\n--- got ---\n%s\n--- want ---\n%s\n(run `go test ./internal/indexer/goldeniir -update-iir-golden` if the change is intended)", fx, got, want)
	}
}
