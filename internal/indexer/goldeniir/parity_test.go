package goldeniir

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/indexer/wasmparse"
	"github.com/atheory-ai/context-engine/internal/plugins/runtime"
)

// TestHostPluginLiftParity is the strangler-fig parity gate (RFC 15
// §Migration step 3): before the host Go TS extractor (internal/iir/extract.go)
// can be retired, the plugin lift must produce the SAME FunctionIntent as the
// host extractor on the TS corpus. For each TS fixture it runs both extractors
// and compares each function pair with iir.Compare — any mismatch means the two
// TS frontends have diverged and `ce iir verify` (host) disagrees with indexing
// (plugin).
func TestHostPluginLiftParity(t *testing.T) {
	ctx := context.Background()

	wasmPath := filepath.Join("..", "defaults", "typescript.wasm")
	if _, err := os.Stat(wasmPath); err != nil {
		t.Skipf("typescript.wasm not built — run `make bundle-default-plugins`")
	}

	parser, err := wasmparse.New(ctx, "")
	if err != nil {
		t.Fatalf("wasmparse.New: %v", err)
	}
	defer parser.Close(ctx)

	ch := core.NewAppChannels()
	rt, err := runtime.New(t.TempDir(), &ch)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	plugin, err := rt.Load(ctx, wasmPath, map[string]any{"enabled": true})
	if err != nil {
		t.Fatalf("load typescript.wasm: %v", err)
	}
	lang := plugin.Language()

	fixtures, _ := filepath.Glob(filepath.Join("testdata", "typescript", "*"))
	for _, fx := range fixtures {
		if filepath.Ext(fx) == ".json" {
			continue
		}
		t.Run(filepath.Base(fx), func(t *testing.T) {
			content, err := os.ReadFile(fx)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			// Host extractor (the code to be retired).
			hostIntents, err := iir.ExtractAll(ctx, content)
			if err != nil {
				t.Fatalf("host ExtractAll: %v", err)
			}
			hostByName := map[string]*iir.FunctionIntent{}
			for _, fi := range hostIntents {
				hostByName[fi.Name] = fi
			}

			// Plugin lift (the replacement).
			treeJSON, err := parser.ParseFile(ctx, fx, content)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			res, err := lang.Extract(filepath.Base(fx), content, treeJSON)
			if err != nil {
				t.Fatalf("plugin Extract: %v", err)
			}

			pluginNames := map[string]bool{}
			for _, e := range res.IIR {
				pf, err := iir.ParseIntentJSON(e.Intent)
				if err != nil {
					t.Fatalf("parse plugin intent: %v", err)
				}
				pluginNames[pf.Name] = true

				hf, ok := hostByName[pf.Name]
				if !ok {
					t.Errorf("plugin lifted %q but host extractor did not", pf.Name)
					continue
				}
				_, mismatches := iir.Compare(hf, pf)
				for _, m := range mismatches {
					t.Errorf("parity mismatch on %q: [%s] %s (host=%v plugin=%v)",
						pf.Name, m.Kind, m.Path, m.Expected, m.Actual)
				}
			}
			// Every function the host sees must also be lifted by the plugin.
			for name := range hostByName {
				if !pluginNames[name] {
					t.Errorf("host extracted %q but plugin lift did not", name)
				}
			}
		})
	}
}
