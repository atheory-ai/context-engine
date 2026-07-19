package writebuffer_test

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/storage/queries"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

func TestIIRUpsert_WritesAndReplaces(t *testing.T) {
	graphDB := setupGraphDB(t)
	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)

	const id = "iir-1"
	send := func(payload string, updatedAt int64) {
		op := writebuffer.WriteOp{
			Type:      writebuffer.OpUpsertIIR,
			ProjectID: "proj1",
			Payload: writebuffer.IIRUpsert{
				ID: id, ProjectID: "proj1", NodeID: "node1", Kind: queries.IIRKindExtracted,
				Language: "typescript", IIR: payload, SourceHash: "h1", RunID: "run1",
				CreatedAt: 1, UpdatedAt: updatedAt,
			},
		}
		if err := buf.Send(ctx, op); err != nil {
			t.Fatalf("send: %v", err)
		}
		if err := buf.Flush(ctx); err != nil {
			t.Fatalf("flush: %v", err)
		}
	}

	send(`{"name":"f"}`, 1)

	var payload string
	var updatedAt int64
	if err := graphDB.QueryRow(
		`SELECT iir, updated_at FROM iir WHERE id = ?`, id,
	).Scan(&payload, &updatedAt); err != nil {
		t.Fatalf("read after insert: %v", err)
	}
	if payload != `{"name":"f"}` {
		t.Errorf("payload = %q", payload)
	}

	// Same id, new payload → upsert replaces in place (idempotent by ID).
	send(`{"name":"f","changed":true}`, 2)

	var count int
	if err := graphDB.QueryRow(`SELECT COUNT(*) FROM iir WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected a single upserted row, got %d", count)
	}
	if err := graphDB.QueryRow(
		`SELECT iir, updated_at FROM iir WHERE id = ?`, id,
	).Scan(&payload, &updatedAt); err != nil {
		t.Fatalf("read after upsert: %v", err)
	}
	if payload != `{"name":"f","changed":true}` || updatedAt != 2 {
		t.Errorf("upsert did not replace: payload=%q updatedAt=%d", payload, updatedAt)
	}
}
