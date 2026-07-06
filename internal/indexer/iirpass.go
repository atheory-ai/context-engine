package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

// nodeTypeSymbol is the node type language plugins use for code symbols
// (functions, classes, …). IIR correlates against the function ones.
const nodeTypeSymbol = "symbol"

// iirLanguageForFile returns the IIR language for a file, if a built-in
// extractor exists for it. Gates which files the IIR pass runs on.
func iirLanguageForFile(relPath string) (string, bool) {
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".ts", ".tsx":
		return "typescript", true
	default:
		return "", false
	}
}

// extractFileIIR extracts IIR for one file and writes one 'extracted' record per
// function that correlates to a symbol node. Best-effort: a parse/extract
// failure is warned and skipped, never failing the file's indexing.
func (idx *Indexer) extractFileIIR(
	ctx context.Context,
	projectID core.ProjectID,
	relPath, sourceHash string,
	content []byte,
	tree *sitter.Tree,
	symbolNodes []core.Node,
	now int64,
) {
	lang, ok := iirLanguageForFile(relPath)
	if !ok {
		return
	}
	if tree == nil {
		return // no grammar parse to reuse
	}
	intents, err := iir.ExtractAllFromNode(tree.RootNode(), content)
	if err != nil {
		idx.emitWarning(fmt.Sprintf("iir extract %s: %v", relPath, err))
		return
	}
	records, err := correlateIIR(projectID, lang, sourceHash, intents, symbolNodes, now)
	if err != nil {
		idx.emitWarning(fmt.Sprintf("iir correlate %s: %v", relPath, err))
		return
	}
	for _, rec := range records {
		if err := idx.substrate.UpsertIIR(ctx, rec); err != nil {
			idx.emitWarning(fmt.Sprintf("write iir for %s: %v", rec.NodeID, err))
		}
	}
}

// nameByte keys a symbol node by its name and start byte for exact correlation.
type nameByte struct {
	name      string
	startByte uint32
}

// correlateIIR matches each extracted function to its structural symbol node and
// builds an 'extracted' IIRRecord per match. Matching is exact by
// (name, start_byte) — the plugin and IIR anchor start_byte on the same
// declaration node — which disambiguates same-named functions (overloads). When
// no exact byte match is found but the name is unique among symbol nodes, that
// node is used (tolerates any anchor drift). A same-named function with no exact
// match is skipped rather than attached to the wrong node. Pure: no I/O.
func correlateIIR(
	projectID core.ProjectID,
	language, sourceHash string,
	fns []iir.ExtractedFunction,
	symbolNodes []core.Node,
	now int64,
) ([]core.IIRRecord, error) {
	byNameByte := make(map[nameByte]core.NodeID)
	byName := make(map[string][]core.NodeID)
	for _, n := range symbolNodes {
		if n.Type != nodeTypeSymbol {
			continue
		}
		byName[n.Label] = append(byName[n.Label], n.ID)
		if sb, ok := nodeStartByte(n); ok {
			byNameByte[nameByte{n.Label, sb}] = n.ID
		}
	}

	out := make([]core.IIRRecord, 0, len(fns))
	for _, f := range fns {
		nodeID, ok := resolveSymbolNode(f, byNameByte, byName)
		if !ok {
			continue
		}
		payload, err := json.Marshal(f.Intent)
		if err != nil {
			return nil, fmt.Errorf("marshal intent %q: %w", f.Intent.Name, err)
		}
		out = append(out, core.IIRRecord{
			ProjectID:  projectID,
			NodeID:     nodeID,
			Kind:       queries.IIRKindExtracted,
			Language:   language,
			Payload:    string(payload),
			SourceHash: sourceHash,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	return out, nil
}

// resolveSymbolNode picks the node for an extracted function: exact
// (name, start_byte) first, then a unique-name fallback; ambiguous otherwise.
func resolveSymbolNode(
	f iir.ExtractedFunction,
	byNameByte map[nameByte]core.NodeID,
	byName map[string][]core.NodeID,
) (core.NodeID, bool) {
	if id, ok := byNameByte[nameByte{f.Intent.Name, f.StartByte}]; ok {
		return id, true
	}
	if ids := byName[f.Intent.Name]; len(ids) == 1 {
		return ids[0], true
	}
	return "", false
}

// nodeStartByte reads a symbol node's start_byte property. It arrives as a JSON
// number (float64) across the plugin boundary; uint32 covers an in-process
// producer. A negative or out-of-range value is rejected.
func nodeStartByte(n core.Node) (uint32, bool) {
	switch v := n.Properties["start_byte"].(type) {
	case float64:
		if v < 0 || v > math.MaxUint32 {
			return 0, false
		}
		return uint32(v), true
	case uint32:
		return v, true
	default:
		return 0, false
	}
}
