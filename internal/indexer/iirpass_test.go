package indexer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

// captureWriter records UpsertIIR calls; other SubstrateWriter methods are
// unused by writePluginIIR (the embedded nil interface would panic if called —
// the test asserts only IIR writes happen).
type captureWriter struct {
	core.SubstrateWriter
	records []core.IIRRecord
}

func (w *captureWriter) UpsertIIR(_ context.Context, r core.IIRRecord) error {
	w.records = append(w.records, r)
	return nil
}

// The plugin lift path (Track B) stores validated intents by their
// plugin-provided node id, skips malformed ones, and writes a canonical payload.
func TestWritePluginIIR_StoresValidatedIntentsByNodeID(t *testing.T) {
	w := &captureWriter{}
	ch := core.NewAppChannels()
	idx := &Indexer{substrate: w, channels: &ch}

	valid := `{"kind":"FunctionIntent","name":"findUser","language":"typescript",` +
		`"origin":"observed","visibility":"public","inputs":[{"name":"id","type":"string"}],` +
		`"returns":{"type":"User","explicit":true},"behavior":[],` +
		`"sideEffects":[],"failureModes":[],"constraints":[]}`
	entries := []core.IIRExtracted{
		{NodeID: "node-1", Intent: json.RawMessage(valid)},
		{NodeID: "node-2", Intent: json.RawMessage(`{ broken json`)}, // skipped
	}

	idx.writePluginIIR(context.Background(), "proj", "hash1", entries, 42)

	if len(w.records) != 1 {
		t.Fatalf("expected 1 record (malformed skipped), got %d", len(w.records))
	}
	r := w.records[0]
	if r.NodeID != "node-1" || r.Language != "typescript" || r.Kind != queries.IIRKindExtracted {
		t.Errorf("record = %+v", r)
	}
	if r.ProjectID != "proj" || r.SourceHash != "hash1" || r.CreatedAt != 42 {
		t.Errorf("record meta = %+v", r)
	}
	back, err := iir.ParseIntentJSON([]byte(r.Payload))
	if err != nil || back.Name != "findUser" {
		t.Errorf("stored payload not canonical FunctionIntent: %v %+v", err, back)
	}
}
