package indexer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

// writePluginIIR stores IIR a language plugin lifted and attached to its own
// symbol nodes (Track B). The plugin already correlated each intent to its node
// id, so no (name, start_byte) matching is needed. Each intent passes the same
// deterministic gate hand-authored IIR does (ParseIntentJSON); a malformed one
// is warned and skipped. Best-effort — never fails the file's indexing.
//
// This is the sole index-time IIR path: the host no longer runs its own Go
// extractor during indexing (the single-function extractor in internal/iir
// remains for the standalone verify/host surfaces). IIR at index time is owned
// entirely by language plugins.
func (idx *Indexer) writePluginIIR(
	ctx context.Context,
	projectID core.ProjectID,
	sourceHash string,
	entries []core.IIRExtracted,
	now int64,
) {
	for _, e := range entries {
		intent, err := iir.ParseIntentJSON(e.Intent)
		if err != nil {
			idx.emitWarning(fmt.Sprintf("plugin iir for %s: %v", e.NodeID, err))
			continue
		}
		// Re-marshal the validated intent so the stored payload is canonical.
		payload, err := json.Marshal(intent)
		if err != nil {
			idx.emitWarning(fmt.Sprintf("plugin iir marshal %s: %v", e.NodeID, err))
			continue
		}
		if err := idx.substrate.UpsertIIR(ctx, core.IIRRecord{
			ProjectID:  projectID,
			NodeID:     e.NodeID,
			Kind:       queries.IIRKindExtracted,
			Language:   intent.Language,
			Payload:    string(payload),
			SourceHash: sourceHash,
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			idx.emitWarning(fmt.Sprintf("write plugin iir for %s: %v", e.NodeID, err))
		}
	}
}
