package writebuffer

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

const (
	DefaultBufferSize    = 1024 // unique ops before forced flush
	DefaultFlushInterval = 50 * time.Millisecond
)

// Buffer is the single-writer goroutine interface.
// Callers obtain a Buffer from New() and call Send().
// Send never blocks (buffered channel). If the channel is full,
// Send returns ErrBufferFull — callers should log and continue.
type Buffer interface {
	// Send enqueues a write operation. Non-blocking.
	Send(op WriteOp) error

	// Flush forces an immediate flush. Blocks until complete.
	// Used at the end of a query turn to ensure all writes land.
	Flush(ctx context.Context) error

	// Close flushes remaining ops and shuts down the goroutine.
	Close(ctx context.Context) error
}

// ErrBufferFull is returned by Send when the internal channel is full.
const errBufferFull = "write buffer full"

// buffer is the concrete Buffer implementation.
type buffer struct {
	ch            chan WriteOp
	pending       *pendingMap
	dbProvider    DBProvider
	flushReq      chan struct{} // caller → goroutine: "please flush now"
	flushDone     chan struct{} // goroutine → caller: "flush complete"
	bufSize       int
	flushInterval time.Duration
	cancel        context.CancelFunc
	done          chan struct{} // closed when goroutine exits
}

// New creates a Buffer and starts the writer goroutine.
// dbProvider resolves a project ID to the appropriate *sql.DB.
// The goroutine runs until Close is called.
func New(
	ctx context.Context,
	dbProvider DBProvider,
	bufSize int,
	flushInterval time.Duration,
) Buffer {
	innerCtx, cancel := context.WithCancel(ctx)
	b := &buffer{
		ch:            make(chan WriteOp, bufSize),
		pending:       newPendingMap(),
		dbProvider:    dbProvider,
		flushReq:      make(chan struct{}),
		flushDone:     make(chan struct{}),
		bufSize:       bufSize,
		flushInterval: flushInterval,
		cancel:        cancel,
		done:          make(chan struct{}),
	}
	go b.run(innerCtx)
	return b
}

// Send enqueues a write operation. Non-blocking.
// Returns an error if the internal channel is full.
func (b *buffer) Send(op WriteOp) error {
	select {
	case b.ch <- op:
		return nil
	default:
		return fmt.Errorf(errBufferFull)
	}
}

// Flush forces an immediate flush. Blocks until the goroutine has completed
// the flush or the context is cancelled.
func (b *buffer) Flush(ctx context.Context) error {
	select {
	case b.flushReq <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-b.flushDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close cancels the goroutine (which drains remaining ops) and waits for exit.
func (b *buffer) Close(ctx context.Context) error {
	b.cancel()
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run is the writer goroutine. It merges incoming ops into the pending map
// and flushes on three triggers: buffer full, timer, or explicit flush request.
func (b *buffer) run(ctx context.Context) {
	defer close(b.done)

	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case op := <-b.ch:
			b.pending.merge(op)
			if b.pending.len() >= b.bufSize {
				b.doFlush(ctx)
			}

		case <-ticker.C:
			if b.pending.len() > 0 {
				b.doFlush(ctx)
			}

		case <-b.flushReq:
			// Drain the channel first so all sent ops are included in this flush.
			b.drainCh()
			b.doFlush(ctx)
			b.flushDone <- struct{}{}

		case <-ctx.Done():
			// Drain channel and pending map before exit.
		drain:
			for {
				select {
				case op := <-b.ch:
					b.pending.merge(op)
				default:
					break drain
				}
			}
			b.doFlush(context.Background())
			return
		}
	}
}

// drainCh reads all available ops from the input channel into the pending map.
// Non-blocking — returns immediately when the channel has no more buffered ops.
func (b *buffer) drainCh() {
	for {
		select {
		case op := <-b.ch:
			b.pending.merge(op)
		default:
			return
		}
	}
}

// doFlush drains the pending map and writes all ops to their respective databases.
func (b *buffer) doFlush(ctx context.Context) {
	ops := b.pending.drain()
	if len(ops) == 0 {
		return
	}

	// Group ops by project ID — each project gets one transaction.
	byProject := make(map[string][]WriteOp, 4)
	for _, op := range ops {
		byProject[op.ProjectID] = append(byProject[op.ProjectID], op)
	}

	for projectID, projectOps := range byProject {
		db, err := b.dbProvider.GraphDB(projectID)
		if err != nil {
			log.Printf("writebuffer: GraphDB(%q): %v", projectID, err)
			continue
		}
		if err := flushProject(ctx, db, projectOps); err != nil {
			log.Printf("writebuffer: flush project %q: %v", projectID, err)
		}
	}
}

// flushProject writes all ops for one project in a single transaction.
func flushProject(ctx context.Context, db *sql.DB, ops []WriteOp) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, op := range ops {
		if err := execOp(ctx, tx, op); err != nil {
			return fmt.Errorf("exec %s: %w", op.Type, err)
		}
	}
	return tx.Commit()
}

// execOp executes a single write op against the transaction.
func execOp(ctx context.Context, tx *sql.Tx, op WriteOp) error {
	switch op.Type {
	case OpUpsertNode:
		n := op.Payload.(NodeUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, plugin_id, created_at, updated_at, properties)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				label        = excluded.label,
				source_class = excluded.source_class,
				updated_at   = excluded.updated_at,
				properties   = excluded.properties
		`, n.ID, n.ProjectID, n.Type, n.Label, n.CanonicalID, n.SourceClass,
			nullableString(n.PluginID), n.CreatedAt, n.UpdatedAt, n.Properties)
		return err

	case OpUpsertEdge:
		e := op.Payload.(EdgeUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO edges (id, project_id, source_id, target_id, type, source_class, plugin_id, created_at, properties)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				source_class = excluded.source_class,
				properties   = excluded.properties
		`, e.ID, e.ProjectID, e.SourceID, e.TargetID, e.Type, e.SourceClass,
			nullableString(e.PluginID), e.CreatedAt, e.Properties)
		return err

	case OpUpdateActivation:
		a := op.Payload.(ActivationUpdate)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO node_activation (node_id, activation, peak_activation, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(node_id) DO UPDATE SET
				activation      = excluded.activation,
				peak_activation = MAX(node_activation.peak_activation, excluded.activation),
				updated_at      = excluded.updated_at
		`, a.NodeID, a.Activation, a.Activation, a.UpdatedAt)
		return err

	case OpUpdateWeight:
		w := op.Payload.(WeightUpdate)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO edge_weight (edge_id, weight, source_class, co_activation_count, last_co_activation, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(edge_id) DO UPDATE SET
				weight              = excluded.weight,
				source_class        = excluded.source_class,
				co_activation_count = edge_weight.co_activation_count + excluded.co_activation_count,
				last_co_activation  = excluded.last_co_activation,
				updated_at          = excluded.updated_at
		`, w.EdgeID, w.Weight, w.SourceClass, w.CoActivationDelta, w.UpdatedAt, w.UpdatedAt)
		return err

	case OpUpsertConcept:
		c := op.Payload.(ConceptUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO concept_seeds (id, term, scope, definition, related, synonyms, source, plugin_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				definition = excluded.definition,
				related    = excluded.related,
				synonyms   = excluded.synonyms,
				updated_at = excluded.updated_at
		`, c.ID, c.Term, c.Scope, nullableString(c.Definition),
			c.Related, c.Synonyms, c.Source, nullableString(c.PluginID),
			c.CreatedAt, c.UpdatedAt)
		return err

	case OpRecordEnrichment:
		e := op.Payload.(EnrichmentRecord)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO enrichments (id, run_id, turn_id, loop_index, entity_type, entity_id, action, before_state, after_state, rationale, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, e.ID, e.RunID, e.TurnID, e.LoopIndex, e.EntityType, e.EntityID,
			e.Action, e.BeforeState, e.AfterState, e.Rationale, e.CreatedAt)
		return err
	}

	return fmt.Errorf("unknown op type: %q", op.Type)
}

// nullableString returns nil for empty strings (stored as SQL NULL).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
