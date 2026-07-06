package indexer

import (
	"context"
	"encoding/json"
	"fmt"
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

// correlateIIR matches each extracted intent to a function symbol node by name
// and builds an 'extracted' IIRRecord per match. Intents with no matching node
// (e.g. a name the plugin didn't surface as a symbol) are skipped — a function
// with no node has nowhere to attach. Pure: no I/O.
func correlateIIR(
	projectID core.ProjectID,
	language, sourceHash string,
	intents []*iir.FunctionIntent,
	symbolNodes []core.Node,
	now int64,
) ([]core.IIRRecord, error) {
	// Build a name → node index. A name mapping to more than one symbol node
	// (overloads, or same-named functions the plugin aggregated) is ambiguous:
	// without a location we can't tell which node an intent belongs to, so we
	// mark it and skip rather than risk attaching intent to the wrong symbol.
	// (Location-based disambiguation is a later SDK follow-up — see the RFC.)
	nodeByName := make(map[string]core.NodeID, len(symbolNodes))
	ambiguous := make(map[string]bool)
	for _, n := range symbolNodes {
		if n.Type != nodeTypeSymbol {
			continue
		}
		if _, seen := nodeByName[n.Label]; seen {
			ambiguous[n.Label] = true
			continue
		}
		nodeByName[n.Label] = n.ID
	}

	out := make([]core.IIRRecord, 0, len(intents))
	for _, fi := range intents {
		if ambiguous[fi.Name] {
			continue
		}
		nodeID, ok := nodeByName[fi.Name]
		if !ok {
			continue
		}
		payload, err := json.Marshal(fi)
		if err != nil {
			return nil, fmt.Errorf("marshal intent %q: %w", fi.Name, err)
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
