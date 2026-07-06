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

func ef(name string, startByte uint32) iir.ExtractedFunction {
	return iir.ExtractedFunction{
		Intent:    &iir.FunctionIntent{Kind: iir.KindFunctionIntent, Name: name},
		StartByte: startByte,
	}
}

func symNode(id, label string, startByte uint32) core.Node {
	return core.Node{
		ID: core.NodeID(id), Type: "symbol", Label: label,
		Properties: map[string]any{"start_byte": float64(startByte)}, // JSON number
	}
}

func TestCorrelateIIR_MatchesByNameAndStartByte(t *testing.T) {
	fns := []iir.ExtractedFunction{
		ef("a", 10),
		ef("b", 40),
		ef("orphan", 99), // no node → skipped
	}
	nodes := []core.Node{
		symNode("n-a", "a", 10),
		symNode("n-b", "b", 40),
		{ID: "n-file", Type: "file", Label: "a"}, // non-symbol → ignored
	}

	recs, err := correlateIIR("proj", "typescript", "hash1", fns, nodes, 100)
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
	if !strings.Contains(rec.Payload, `"name":"a"`) {
		t.Errorf("payload should embed the FunctionIntent JSON: %s", rec.Payload)
	}
}

func TestCorrelateIIR_ByteDisambiguatesSameName(t *testing.T) {
	// Two functions named "dup" at different bytes — exactly what name-only
	// correlation could not resolve. Both now correlate to the right node.
	fns := []iir.ExtractedFunction{ef("dup", 10), ef("dup", 50)}
	nodes := []core.Node{symNode("n-dup-1", "dup", 10), symNode("n-dup-2", "dup", 50)}

	recs, err := correlateIIR("proj", "typescript", "h", fns, nodes, 1)
	if err != nil {
		t.Fatalf("correlateIIR: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want both dup functions correlated, got %+v", recs)
	}
	got := map[uint32]core.NodeID{10: recs[0].NodeID, 50: recs[1].NodeID}
	if got[10] != "n-dup-1" || got[50] != "n-dup-2" {
		t.Errorf("byte correlation attached to the wrong nodes: %+v", recs)
	}
}

func TestCorrelateIIR_UniqueNameFallbackOnByteDrift(t *testing.T) {
	// The node's byte doesn't match the extracted byte (anchor drift), but the
	// name is unique → still correlates.
	fns := []iir.ExtractedFunction{ef("solo", 7)}
	nodes := []core.Node{symNode("n-solo", "solo", 999)}
	recs, err := correlateIIR("p", "typescript", "", fns, nodes, 1)
	if err != nil {
		t.Fatalf("correlateIIR: %v", err)
	}
	if len(recs) != 1 || recs[0].NodeID != "n-solo" {
		t.Fatalf("expected unique-name fallback to correlate, got %+v", recs)
	}
}

func TestCorrelateIIR_AmbiguousNoByteMatchSkipped(t *testing.T) {
	// Two "dup" nodes, extracted "dup" at a byte matching neither → skipped
	// rather than attached to a guessed node.
	fns := []iir.ExtractedFunction{ef("dup", 99)}
	nodes := []core.Node{symNode("n-dup-1", "dup", 10), symNode("n-dup-2", "dup", 50)}
	recs, err := correlateIIR("p", "typescript", "", fns, nodes, 1)
	if err != nil {
		t.Fatalf("correlateIIR: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("ambiguous same-name with no byte match must be skipped: %+v", recs)
	}
}

func TestCorrelateIIR_IgnoresNonSymbolNodes(t *testing.T) {
	fns := []iir.ExtractedFunction{ef("a", 0)}
	nodes := []core.Node{{ID: "f", Type: "file", Label: "a"}}
	recs, err := correlateIIR("p", "typescript", "", fns, nodes, 1)
	if err != nil {
		t.Fatalf("correlateIIR: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("a non-symbol node must not match a function intent: %+v", recs)
	}
}
