package goldeniir

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/indexer/wasmparse"
	"github.com/atheory-ai/context-engine/internal/plugins/runtime"
)

// TestSelfIndexSmoke dogfoods the pipeline on CE's own Go source: it runs the Go
// plugin's lift over internal/iir/*.go (a stable, function-rich package) and
// asserts the real code extracts cleanly. This is a smoke/coverage check, not a
// byte-golden — CE's source evolves, so it validates "extracts many valid
// intents with zero parse/lift errors" rather than an exact fixture.
func TestSelfIndexSmoke(t *testing.T) {
	ctx := context.Background()
	wasmPath := filepath.Join("..", "defaults", "go-language.wasm")
	if _, err := os.Stat(wasmPath); err != nil {
		t.Skipf("go-language plugin not built — run `make bundle-default-plugins`")
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
		t.Fatalf("load go plugin: %v", err)
	}
	lang := plugin.Language()

	files, err := filepath.Glob(filepath.Join("..", "..", "iir", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no source to dogfood: %v", err)
	}

	var intents, filesWithIIR int
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		treeJSON, err := parser.ParseFile(ctx, f, content)
		if err != nil {
			t.Errorf("parse %s: %v", f, err)
			continue
		}
		res, err := lang.Extract(filepath.Base(f), content, treeJSON)
		if err != nil {
			t.Errorf("extract %s: %v", f, err)
			continue
		}
		for _, e := range res.IIR {
			if _, err := iir.ParseIntentJSON(e.Intent); err != nil {
				t.Errorf("%s: invalid intent %s: %v", f, e.Intent, err)
			}
			intents++
		}
		if len(res.IIR) > 0 {
			filesWithIIR++
		}
	}

	// internal/iir is a large package; a healthy lift finds many functions.
	if intents < 30 {
		t.Errorf("dogfood extracted only %d intents from CE's own Go (expected many) — lift may be regressing", intents)
	}
	t.Logf("dogfood: %d valid intents across %d files", intents, filesWithIIR)
}
