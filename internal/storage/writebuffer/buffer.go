package writebuffer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultBufferSize    = 1024 // unique ops before forced flush
	DefaultFlushInterval = 50 * time.Millisecond
)

// Buffer is the single-writer goroutine interface.
// Callers obtain a Buffer from New() and call Send().
// Send applies backpressure while the queue is full and returns when its
// context is cancelled or a prior database flush has failed.
type Buffer interface {
	// Send enqueues a write operation, waiting for capacity when necessary.
	Send(ctx context.Context, op WriteOp) error

	// Flush forces an immediate flush. Blocks until complete.
	// Used at the end of a query turn to ensure all writes land.
	Flush(ctx context.Context) error

	// BeginIndexTransaction holds index writes in the buffer after first
	// flushing older work. CommitIndexTransaction flushes the complete held set
	// in one project transaction; AbortIndexTransaction drops it.
	BeginIndexTransaction(ctx context.Context) error
	CommitIndexTransaction(ctx context.Context) error
	AbortIndexTransaction(ctx context.Context) error

	// Close flushes remaining ops and shuts down the goroutine.
	Close(ctx context.Context) error
}

const errBufferClosed = "write buffer closed"

// buffer is the concrete Buffer implementation.
type buffer struct {
	ch            chan WriteOp
	pending       *pendingMap
	dbProvider    DBProvider
	flushReq      chan chan error // caller → goroutine: "please flush now"
	indexReq      chan indexControl
	bufSize       int
	flushInterval time.Duration
	cancel        context.CancelFunc
	done          chan struct{} // closed when goroutine exits
	errMu         sync.Mutex
	err           error // first storage failure; makes the buffer fail closed
}

type indexControl struct {
	action   string
	response chan error
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
		flushReq:      make(chan chan error),
		indexReq:      make(chan indexControl),
		bufSize:       bufSize,
		flushInterval: flushInterval,
		cancel:        cancel,
		done:          make(chan struct{}),
	}
	go b.run(innerCtx)
	return b
}

func (b *buffer) BeginIndexTransaction(ctx context.Context) error {
	return b.indexControl(ctx, "begin")
}
func (b *buffer) CommitIndexTransaction(ctx context.Context) error {
	return b.indexControl(ctx, "commit")
}
func (b *buffer) AbortIndexTransaction(ctx context.Context) error {
	return b.indexControl(ctx, "abort")
}
func (b *buffer) indexControl(ctx context.Context, action string) error {
	if err := b.failure(); err != nil {
		return err
	}
	response := make(chan error, 1)
	select {
	case b.indexReq <- indexControl{action: action, response: response}:
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		return errors.New(errBufferClosed)
	}
	select {
	case err := <-response:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Send enqueues a write operation. It blocks only while the bounded channel is
// full, preserving every accepted write instead of dropping work under load.
func (b *buffer) Send(ctx context.Context, op WriteOp) error {
	if err := b.failure(); err != nil {
		return err
	}
	select {
	case b.ch <- op:
		return b.failure()
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		if err := b.failure(); err != nil {
			return err
		}
		return errors.New(errBufferClosed)
	}
}

// Flush forces an immediate flush. Blocks until the goroutine has completed
// the flush or the context is cancelled.
func (b *buffer) Flush(ctx context.Context) error {
	if err := b.failure(); err != nil {
		return err
	}
	response := make(chan error, 1)
	select {
	case b.flushReq <- response:
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		if err := b.failure(); err != nil {
			return err
		}
		return errors.New(errBufferClosed)
	}
	select {
	case err := <-response:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close cancels the goroutine (which drains remaining ops) and waits for exit.
func (b *buffer) Close(ctx context.Context) error {
	b.cancel()
	select {
	case <-b.done:
		// Close is lifecycle cleanup. Callers that need delivery confirmation use
		// Flush, which returns any sticky storage failure before shutdown.
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

	held := false
	for {
		select {
		case op := <-b.ch:
			if b.failure() == nil {
				b.pending.merge(op)
				if !held && b.pending.len() >= b.bufSize {
					b.setFailure(b.doFlush(ctx))
				}
			}

		case <-ticker.C:
			if !held && b.pending.len() > 0 && b.failure() == nil {
				b.setFailure(b.doFlush(ctx))
			}

		case response := <-b.flushReq:
			// Drain the channel first so all sent ops are included in this flush.
			if b.failure() == nil {
				b.drainCh()
				b.setFailure(b.doFlush(ctx))
			}
			response <- b.failure()

		case req := <-b.indexReq:
			var err error
			switch req.action {
			case "begin":
				if held {
					err = errors.New("index transaction already active")
				} else if b.pending.len() > 0 {
					err = b.doFlush(ctx)
				}
				if err == nil {
					held = true
				}
			case "commit":
				if !held {
					err = errors.New("no active index transaction")
				} else {
					b.drainCh()
					err = b.doFlush(ctx)
					held = false
				}
			case "abort":
				if !held {
					err = errors.New("no active index transaction")
				} else {
					b.drainCh()
					_ = b.pending.drain()
					held = false
				}
			default:
				err = errors.New("unknown index transaction action")
			}
			b.setFailure(err)
			req.response <- b.failure()

		case <-ctx.Done():
			// Drain channel and pending map before exit.
		drain:
			for {
				select {
				case op := <-b.ch:
					if b.failure() == nil {
						b.pending.merge(op)
					}
				default:
					break drain
				}
			}
			if b.failure() == nil {
				b.setFailure(b.doFlush(context.Background()))
			}
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
func (b *buffer) doFlush(ctx context.Context) error {
	ops := b.pending.drain()
	if len(ops) == 0 {
		return nil
	}

	// Group ops by project ID — each project gets one transaction.
	byProject := make(map[string][]WriteOp, 4)
	for _, op := range ops {
		byProject[op.ProjectID] = append(byProject[op.ProjectID], op)
	}

	for projectID, projectOps := range byProject {
		db, err := b.dbProvider.GraphDB(projectID)
		if err != nil {
			return fmt.Errorf("graph db %q: %w", projectID, err)
		}
		if err := flushProject(ctx, db, projectOps); err != nil {
			return fmt.Errorf("flush project %q: %w", projectID, err)
		}
	}
	return nil
}

func (b *buffer) failure() error {
	b.errMu.Lock()
	defer b.errMu.Unlock()
	return b.err
}

func (b *buffer) setFailure(err error) {
	if err == nil {
		return
	}
	b.errMu.Lock()
	defer b.errMu.Unlock()
	if b.err == nil {
		b.err = err
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
			INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, plugin_id, source_file, index_managed, last_index_run_id, created_at, updated_at, properties)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				label        = excluded.label,
				source_class = excluded.source_class,
				source_file  = excluded.source_file,
				index_managed = excluded.index_managed,
				last_index_run_id = excluded.last_index_run_id,
				updated_at   = excluded.updated_at,
				properties   = excluded.properties
		`, n.ID, n.ProjectID, n.Type, n.Label, n.CanonicalID, n.SourceClass,
			nullableString(n.PluginID), n.SourceFile, n.IndexManaged, nullableString(n.LastIndexRunID), n.CreatedAt, n.UpdatedAt, n.Properties)
		return err

	case OpUpsertEdge:
		e := op.Payload.(EdgeUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO edges (id, project_id, source_id, target_id, type, source_class, plugin_id, index_managed, last_index_run_id, created_at, properties)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				source_class = excluded.source_class,
				index_managed = excluded.index_managed,
				last_index_run_id = excluded.last_index_run_id,
				properties   = excluded.properties
		`, e.ID, e.ProjectID, e.SourceID, e.TargetID, e.Type, e.SourceClass,
			nullableString(e.PluginID), e.IndexManaged, nullableString(e.LastIndexRunID), e.CreatedAt, e.Properties)
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

	case OpUpsertIIR:
		r := op.Payload.(IIRUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO iir (id, project_id, node_id, kind, language, iir, source_hash, run_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				language    = excluded.language,
				iir         = excluded.iir,
				source_hash = excluded.source_hash,
				run_id      = excluded.run_id,
				updated_at  = excluded.updated_at
		`, r.ID, r.ProjectID, r.NodeID, r.Kind, r.Language, r.IIR,
			nullableString(r.SourceHash), nullableString(r.RunID), r.CreatedAt, r.UpdatedAt)
		return err

	case OpUpsertSemanticPlan:
		r := op.Payload.(SemanticPlanUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO semantic_plans (id, project_id, unit_id, unit_node_id, parent_plan_id, revision, lifecycle, schema_version, payload, run_id, turn_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, r.ID, r.ProjectID, r.UnitID, nullableString(r.UnitNodeID), nullableString(r.ParentPlanID), r.Revision, r.Lifecycle, r.SchemaVersion, r.Payload, nullableString(r.RunID), nullableString(r.TurnID), r.CreatedAt)
		return err

	case OpUpsertSemanticRecipe:
		r := op.Payload.(SemanticRecipeUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO semantic_recipes (id, project_id, plan_revision_id, schema_version, target_language, renderer_profile, payload, run_id, turn_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, r.ID, r.ProjectID, r.PlanRevisionID, r.SchemaVersion, r.TargetLanguage, r.RendererProfile, r.Payload, nullableString(r.RunID), nullableString(r.TurnID), r.CreatedAt)
		return err

	case OpUpsertSemanticArtifact:
		r := op.Payload.(SemanticArtifactUpsert)
		allowed := 0
		if r.SourceContentAllowed {
			allowed = 1
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO semantic_artifacts (id, project_id, plan_revision_id, recipe_id, unit_node_id, kind, content_hash, target_language, target_path, source_ref, source_content, source_content_allowed, run_id, turn_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, r.ID, r.ProjectID, r.PlanRevisionID, r.RecipeID, nullableString(r.UnitNodeID), r.Kind, r.ContentHash, r.TargetLanguage, r.TargetPath, nullableString(r.SourceRef), nullableString(r.SourceContent), allowed, nullableString(r.RunID), nullableString(r.TurnID), r.CreatedAt)
		return err

	case OpRecordSemanticVerification:
		r := op.Payload.(SemanticVerificationRecord)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO semantic_verifications (id, project_id, plan_revision_id, recipe_id, artifact_id, observed_iir_id, verdict, verifier_version, payload, run_id, turn_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, r.ID, r.ProjectID, r.PlanRevisionID, r.RecipeID, nullableString(r.ArtifactID), nullableString(r.ObservedIIRID), r.Verdict, r.VerifierVersion, r.Payload, nullableString(r.RunID), nullableString(r.TurnID), r.CreatedAt)
		return err

	case OpRecordSemanticApproval:
		r := op.Payload.(SemanticApprovalRecord)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO semantic_approvals (id, project_id, plan_revision_id, scope, decision, rationale, actor_id, run_id, turn_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, r.ID, r.ProjectID, r.PlanRevisionID, r.Scope, r.Decision, r.Rationale, r.ActorID, nullableString(r.RunID), nullableString(r.TurnID), r.CreatedAt)
		return err

	case OpUpsertSemanticTestPlan:
		r := op.Payload.(SemanticTestPlanUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO semantic_test_plans (id, project_id, plan_revision_id, recipe_id, payload, run_id, turn_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, r.ID, r.ProjectID, r.PlanRevisionID, r.RecipeID, r.Payload, nullableString(r.RunID), nullableString(r.TurnID), r.CreatedAt)
		return err

	case OpUpsertSemanticRepair:
		r := op.Payload.(SemanticRepairUpsert)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO semantic_repairs (id, project_id, plan_revision_id, recipe_id, verification_id, status, payload, run_id, turn_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, r.ID, r.ProjectID, r.PlanRevisionID, r.RecipeID, r.VerificationID, r.Status, r.Payload, nullableString(r.RunID), nullableString(r.TurnID), r.CreatedAt)
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
