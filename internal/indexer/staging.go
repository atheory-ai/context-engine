package indexer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

const (
	stageBatchSize     = 256
	stageFlushInterval = 250 * time.Millisecond
)

// stageBatcher serializes the small, durable index manifest writes. It applies
// backpressure through a bounded channel and commits at a predictable size or
// time boundary, so walking/extraction do not create one SQLite transaction per
// file. Graph facts continue through the substrate write buffer.
type stageBatcher struct {
	queries   *queries.IndexQueries
	runID     string
	projectID string
	ctx       context.Context
	cancel    context.CancelFunc
	profile   *Profile
	events    chan queries.StagedFileEvent
	done      chan struct{}

	mu  sync.Mutex
	err error
}

func newStageBatcher(ctx context.Context, cancel context.CancelFunc, q *queries.IndexQueries, runID, projectID string, profile *Profile) *stageBatcher {
	b := &stageBatcher{
		queries:   q,
		runID:     runID,
		projectID: projectID,
		ctx:       ctx,
		cancel:    cancel,
		profile:   profile,
		events:    make(chan queries.StagedFileEvent, stageBatchSize*2),
		done:      make(chan struct{}),
	}
	go b.run()
	return b
}

func (b *stageBatcher) StageWalked(ctx context.Context, path string) error {
	return b.send(ctx, queries.StagedFileEvent{Path: path})
}

func (b *stageBatcher) StageDeleted(ctx context.Context, path string) error {
	return b.send(ctx, queries.StagedFileEvent{Path: path, Deleted: true})
}

func (b *stageBatcher) StageOutput(ctx context.Context, path string, output queries.FileOutput) error {
	return b.send(ctx, queries.StagedFileEvent{Path: path, Output: &output})
}

func (b *stageBatcher) send(ctx context.Context, event queries.StagedFileEvent) error {
	if err := b.Err(); err != nil {
		return err
	}
	select {
	case b.events <- event:
		return b.Err()
	case <-ctx.Done():
		if err := b.Err(); err != nil {
			return err
		}
		return ctx.Err()
	case <-b.done:
		if err := b.Err(); err != nil {
			return err
		}
		return fmt.Errorf("index staging batcher stopped")
	}
}

// Close accepts no further events, flushes the last partial batch, and returns
// the first staging failure. The explicit close flush prevents a dangling final
// buffer when a run ends before the timer fires.
func (b *stageBatcher) Close() error {
	close(b.events)
	<-b.done
	return b.Err()
}

func (b *stageBatcher) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

func (b *stageBatcher) setError(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	if b.err == nil {
		b.err = err
		b.cancel()
	}
	b.mu.Unlock()
}

func (b *stageBatcher) run() {
	defer close(b.done)
	ticker := time.NewTicker(stageFlushInterval)
	defer ticker.Stop()

	batch := make([]queries.StagedFileEvent, 0, stageBatchSize)
	flush := func() {
		if len(batch) == 0 || b.Err() != nil {
			return
		}
		started := time.Now()
		if err := b.queries.StageFileEvents(b.ctx, b.runID, b.projectID, batch); err != nil {
			b.setError(fmt.Errorf("stage index batch: %w", err))
			return
		}
		b.profile.Record("stage.sqlite_batch", time.Since(started))
		b.profile.Add("stage.events", int64(len(batch)))
		batch = batch[:0]
	}
	for {
		select {
		case event, ok := <-b.events:
			if !ok {
				flush()
				return
			}
			if b.Err() == nil {
				batch = append(batch, event)
				if len(batch) >= stageBatchSize {
					flush()
				}
			}
		case <-ticker.C:
			flush()
		}
	}
}
