package indexer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/semantic/lift"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

// writePluginIIR stores IIR a language plugin lifted and attached to its own
// symbol nodes (Track B). The plugin already correlated each intent to its node
// id, so no (name, start_byte) matching is needed. Each intent passes the same
// deterministic gate hand-authored IIR does (ParseIntentJSON). A malformed
// entry fails the file: silently omitting semantic output would otherwise make
// a successful index run incomplete.
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
	runID string,
	now int64,
) ([]string, error) {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		unit, err := lift.Normalize(e)
		if err != nil {
			return nil, fmt.Errorf("plugin source lift for %s: %w", e.NodeID, err)
		}
		// Re-marshal the validated intent so the stored payload is canonical.
		payload, err := json.Marshal(unit.Observed)
		if err != nil {
			return nil, fmt.Errorf("plugin iir marshal %s: %w", e.NodeID, err)
		}
		if err := idx.substrate.UpsertIIR(ctx, core.IIRRecord{
			ProjectID:  projectID,
			NodeID:     e.NodeID,
			Kind:       queries.IIRKindExtracted,
			Language:   unit.Language,
			Payload:    string(payload),
			SourceHash: sourceHash,
			RunID:      runID,
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			return nil, fmt.Errorf("write plugin iir for %s: %w", e.NodeID, err)
		}
		ids = append(ids, queries.IIRID(string(projectID), string(e.NodeID), queries.IIRKindExtracted))
	}
	return ids, nil
}
