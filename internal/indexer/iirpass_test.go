package indexer

import (
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

func TestIIRLanguageForFile(t *testing.T) {
	cases := map[string]bool{
		"src/a.ts":  true,
		"a.tsx":     true,
		"a.go":      false,
		"a.py":      false,
		"README.md": false,
	}
	for path, want := range cases {
		if _, ok := iirLanguageForFile(path); ok != want {
			t.Errorf("iirLanguageForFile(%q) = %v, want %v", path, ok, want)
		}
	}
}

func TestCorrelateIIR_MatchesSymbolByName(t *testing.T) {
	intents := []*iir.FunctionIntent{
		{Kind: iir.KindFunctionIntent, Name: "a"},
		{Kind: iir.KindFunctionIntent, Name: "b"},
		{Kind: iir.KindFunctionIntent, Name: "orphan"}, // no node → skipped
	}
	nodes := []core.Node{
		{ID: "n-a", Type: "symbol", Label: "a"},
		{ID: "n-b", Type: "symbol", Label: "b"},
		{ID: "n-file", Type: "file", Label: "a"}, // non-symbol, same label → ignored
	}

	recs, err := correlateIIR("proj", "typescript", "hash1", intents, nodes, 100)
	if err != nil {
		t.Fatalf("correlateIIR: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records (a, b), got %d: %+v", len(recs), recs)
	}

	byNode := map[core.NodeID]core.IIRRecord{}
	for _, r := range recs {
		byNode[r.NodeID] = r
	}
	rec, ok := byNode["n-a"]
	if !ok {
		t.Fatal("missing record for n-a")
	}
	if rec.Kind != queries.IIRKindExtracted || rec.Language != "typescript" || rec.SourceHash != "hash1" {
		t.Errorf("unexpected record fields: %+v", rec)
	}
	if rec.ProjectID != "proj" || rec.CreatedAt != 100 || rec.UpdatedAt != 100 {
		t.Errorf("unexpected provenance: %+v", rec)
	}
	if !strings.Contains(rec.Payload, `"name":"a"`) {
		t.Errorf("payload should embed the FunctionIntent JSON: %s", rec.Payload)
	}
}

func TestCorrelateIIR_IgnoresNonSymbolNodes(t *testing.T) {
	intents := []*iir.FunctionIntent{{Kind: iir.KindFunctionIntent, Name: "a"}}
	nodes := []core.Node{{ID: "f", Type: "file", Label: "a"}}
	recs, err := correlateIIR("p", "typescript", "", intents, nodes, 1)
	if err != nil {
		t.Fatalf("correlateIIR: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("a non-symbol node must not match a function intent: %+v", recs)
	}
}
