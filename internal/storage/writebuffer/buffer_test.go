package writebuffer_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

// testProvider implements DBProvider for tests using a single in-memory database.
type testProvider struct {
	db *sql.DB
}

type blockingProvider struct {
	db      *sql.DB
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingProvider) GraphDB(_ string) (*sql.DB, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return p.db, nil
}

type failingProvider struct{}

func (failingProvider) GraphDB(_ string) (*sql.DB, error) {
	return nil, errors.New("graph unavailable")
}

func (p *testProvider) GraphDB(_ string) (*sql.DB, error) {
	return p.db, nil
}

// setupGraphDB creates an in-memory database with the graph schema applied.
func setupGraphDB(t *testing.T) *sql.DB {
	t.Helper()
	graphDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	if err := migrations.RunGraph(graphDB); err != nil {
		t.Fatalf("run graph migration: %v", err)
	}
	t.Cleanup(func() { graphDB.Close() })
	return graphDB
}

// insertTestNode inserts a minimal node row for FK-constrained tests.
func insertTestNode(t *testing.T, graphDB *sql.DB, nodeID, projectID string) {
	t.Helper()
	_, err := graphDB.Exec(`
		INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, created_at, updated_at, properties)
		VALUES (?, ?, 'symbol', ?, ?, 'structural', 0, 0, '{}')
	`, nodeID, projectID, nodeID, nodeID)
	if err != nil {
		t.Fatalf("insert test node: %v", err)
	}
}

// insertTestEdge inserts a minimal edge row for FK-constrained tests.
func insertTestEdge(t *testing.T, graphDB *sql.DB, edgeID, projectID, sourceID, targetID string) {
	t.Helper()
	_, err := graphDB.Exec(`
		INSERT INTO edges (id, project_id, source_id, target_id, type, source_class, created_at, properties)
		VALUES (?, ?, ?, ?, 'calls', 'structural', 0, '{}')
	`, edgeID, projectID, sourceID, targetID)
	if err != nil {
		t.Fatalf("insert test edge: %v", err)
	}
}

// newTestBuffer creates a buffer with small flush interval for tests.
func newTestBuffer(ctx context.Context, provider writebuffer.DBProvider) writebuffer.Buffer {
	return writebuffer.New(ctx, provider, 1024, 5*time.Millisecond)
}

func TestActivationDeduplication(t *testing.T) {
	graphDB := setupGraphDB(t)
	insertTestNode(t, graphDB, "node1", "proj1")

	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)

	// Send 10 activation updates for the same node — only the last should persist.
	for i := 0; i < 10; i++ {
		if err := buf.Send(ctx, writebuffer.WriteOp{
			Type:      writebuffer.OpUpdateActivation,
			ProjectID: "proj1",
			Payload: writebuffer.ActivationUpdate{
				NodeID:     "node1",
				Activation: float64(i),
				UpdatedAt:  time.Now().UnixMilli(),
			},
		}); err != nil {
			t.Fatalf("send op %d: %v", i, err)
		}
	}

	if err := buf.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var activation float64
	if err := graphDB.QueryRow(
		`SELECT activation FROM node_activation WHERE node_id = ?`, "node1",
	).Scan(&activation); err != nil {
		t.Fatalf("query activation: %v", err)
	}
	if activation != 9.0 {
		t.Errorf("got activation=%.1f, want 9.0", activation)
	}
}

func TestIndexTransactionStillFlushesBoundedWrites(t *testing.T) {
	graphDB := setupGraphDB(t)
	insertTestNode(t, graphDB, "node1", "proj1")
	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)
	if err := buf.BeginIndexTransaction(ctx); err != nil {
		t.Fatal(err)
	}
	if err := buf.Send(ctx, writebuffer.WriteOp{Type: writebuffer.OpUpdateActivation, ProjectID: "proj1", Payload: writebuffer.ActivationUpdate{NodeID: "node1", Activation: 0.9, UpdatedAt: 1}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond) // exceeds normal auto-flush interval
	var count int
	if err := graphDB.QueryRow(`SELECT COUNT(*) FROM node_activation WHERE node_id = 'node1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("index transaction suppressed the normal timer flush")
	}
	if err := buf.CommitIndexTransaction(ctx); err != nil {
		t.Fatal(err)
	}
	if err := graphDB.QueryRow(`SELECT COUNT(*) FROM node_activation WHERE node_id = 'node1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("committed index write count = %d, want 1", count)
	}
}

func TestIndexManagedGraphWritesStageUntilReconciliation(t *testing.T) {
	graphDB := setupGraphDB(t)
	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)

	for _, op := range []writebuffer.WriteOp{
		{
			Type:      writebuffer.OpUpsertNode,
			ProjectID: "proj1",
			Payload: writebuffer.NodeUpsert{
				ID: "staged-node", ProjectID: "proj1", Type: "symbol", Label: "staged-node", CanonicalID: "file.go:staged-node",
				SourceClass: "structural", IndexManaged: true, LastIndexRunID: "run-1", CreatedAt: 1, UpdatedAt: 1, Properties: "{}",
			},
		},
		{
			Type:      writebuffer.OpUpsertEdge,
			ProjectID: "proj1",
			Payload: writebuffer.EdgeUpsert{
				ID: "staged-edge", ProjectID: "proj1", SourceID: "staged-node", TargetID: "staged-node", Type: "references",
				SourceClass: "structural", IndexManaged: true, LastIndexRunID: "run-1", CreatedAt: 1, Properties: "{}",
			},
		},
	} {
		if err := buf.Send(ctx, op); err != nil {
			t.Fatal(err)
		}
	}
	if err := buf.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if count := queryCount(t, graphDB, `SELECT COUNT(*) FROM index_staging_nodes WHERE run_id = 'run-1'`); count != 1 {
		t.Fatalf("staged nodes = %d, want 1", count)
	}
	if count := queryCount(t, graphDB, `SELECT COUNT(*) FROM index_staging_edges WHERE run_id = 'run-1'`); count != 1 {
		t.Fatalf("staged edges = %d, want 1", count)
	}
	if count := queryCount(t, graphDB, `SELECT COUNT(*) FROM nodes WHERE id = 'staged-node'`); count != 0 {
		t.Fatalf("live nodes = %d, want 0 before reconciliation", count)
	}
}

func queryCount(t *testing.T, graphDB *sql.DB, query string) int {
	t.Helper()
	var count int
	if err := graphDB.QueryRow(query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPeakActivationTracked(t *testing.T) {
	graphDB := setupGraphDB(t)
	insertTestNode(t, graphDB, "node1", "proj1")

	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)

	// Send: 5.0, then 2.0. Peak should be 5.0; current should be 2.0.
	for _, a := range []float64{5.0, 2.0} {
		buf.Send(ctx, writebuffer.WriteOp{
			Type:      writebuffer.OpUpdateActivation,
			ProjectID: "proj1",
			Payload:   writebuffer.ActivationUpdate{NodeID: "node1", Activation: a, UpdatedAt: time.Now().UnixMilli()},
		})
		// Flush between sends so both writes actually hit the DB.
		buf.Flush(ctx)
	}

	var activation, peakActivation float64
	if err := graphDB.QueryRow(
		`SELECT activation, peak_activation FROM node_activation WHERE node_id = ?`, "node1",
	).Scan(&activation, &peakActivation); err != nil {
		t.Fatalf("query: %v", err)
	}
	if activation != 2.0 {
		t.Errorf("got activation=%.1f, want 2.0", activation)
	}
	if peakActivation != 5.0 {
		t.Errorf("got peak_activation=%.1f, want 5.0", peakActivation)
	}
}

func TestWeightCoActivationAccumulation(t *testing.T) {
	graphDB := setupGraphDB(t)
	insertTestNode(t, graphDB, "src1", "proj1")
	insertTestNode(t, graphDB, "tgt1", "proj1")
	insertTestEdge(t, graphDB, "edge1", "proj1", "src1", "tgt1")

	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)

	// Send 3 weight updates with delta=1 each. CoActivationCount should be 3.
	for range 3 {
		buf.Send(ctx, writebuffer.WriteOp{
			Type:      writebuffer.OpUpdateWeight,
			ProjectID: "proj1",
			Payload: writebuffer.WeightUpdate{
				EdgeID:            "edge1",
				Weight:            1.5,
				SourceClass:       "associative",
				CoActivationDelta: 1,
				UpdatedAt:         time.Now().UnixMilli(),
			},
		})
	}

	if err := buf.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var count int
	var weight float64
	if err := graphDB.QueryRow(
		`SELECT co_activation_count, weight FROM edge_weight WHERE edge_id = ?`, "edge1",
	).Scan(&count, &weight); err != nil {
		t.Fatalf("query: %v", err)
	}
	// Deduplication merges the 3 ops into 1 — accumulated delta is 3.
	if count != 3 {
		t.Errorf("got co_activation_count=%d, want 3", count)
	}
	if weight != 1.5 {
		t.Errorf("got weight=%.2f, want 1.5", weight)
	}
}

func TestEnrichmentAlwaysAppends(t *testing.T) {
	graphDB := setupGraphDB(t)

	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)

	// Send 3 enrichment records — all should be written (no deduplication).
	for i := range 3 {
		buf.Send(ctx, writebuffer.WriteOp{
			Type:      writebuffer.OpRecordEnrichment,
			ProjectID: "proj1",
			Payload: writebuffer.EnrichmentRecord{
				ID:         fmt.Sprintf("enr%d", i),
				RunID:      "run1",
				TurnID:     "turn1",
				LoopIndex:  i,
				EntityType: "node",
				EntityID:   "node1",
				Action:     "created",
				AfterState: `{"label":"foo"}`,
				CreatedAt:  time.Now().UnixMilli(),
			},
		})
	}

	if err := buf.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var count int
	if err := graphDB.QueryRow(
		`SELECT COUNT(*) FROM enrichments WHERE run_id = 'run1'`,
	).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 3 {
		t.Errorf("got %d enrichments, want 3", count)
	}
}

func TestNodeUpsertDeduplication(t *testing.T) {
	graphDB := setupGraphDB(t)

	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})
	defer buf.Close(ctx)

	// Send two upserts for the same node — only the final label should win.
	for i, label := range []string{"first", "second"} {
		_ = i
		buf.Send(ctx, writebuffer.WriteOp{
			Type:      writebuffer.OpUpsertNode,
			ProjectID: "proj1",
			Payload: writebuffer.NodeUpsert{
				ID:          "node-dedup",
				ProjectID:   "proj1",
				Type:        "symbol",
				Label:       label,
				CanonicalID: "pkg:Foo",
				SourceClass: "structural",
				Properties:  `{}`,
				CreatedAt:   time.Now().UnixMilli(),
				UpdatedAt:   time.Now().UnixMilli(),
			},
		})
	}

	if err := buf.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var label string
	var nodeCount int
	graphDB.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = 'node-dedup'`).Scan(&nodeCount)
	graphDB.QueryRow(`SELECT label FROM nodes WHERE id = 'node-dedup'`).Scan(&label)

	if nodeCount != 1 {
		t.Errorf("got %d node rows, want 1", nodeCount)
	}
	if label != "second" {
		t.Errorf("got label=%q, want %q", label, "second")
	}
}

func TestCloseFlushesRemaining(t *testing.T) {
	graphDB := setupGraphDB(t)
	insertTestNode(t, graphDB, "nodeX", "proj1")

	ctx := context.Background()
	buf := newTestBuffer(ctx, &testProvider{db: graphDB})

	buf.Send(ctx, writebuffer.WriteOp{
		Type:      writebuffer.OpUpdateActivation,
		ProjectID: "proj1",
		Payload:   writebuffer.ActivationUpdate{NodeID: "nodeX", Activation: 7.0, UpdatedAt: time.Now().UnixMilli()},
	})

	// Close should flush before exiting.
	closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := buf.Close(closeCtx); err != nil {
		t.Fatalf("close: %v", err)
	}

	var activation float64
	if err := graphDB.QueryRow(
		`SELECT activation FROM node_activation WHERE node_id = ?`, "nodeX",
	).Scan(&activation); err != nil {
		t.Fatalf("query: %v", err)
	}
	if activation != 7.0 {
		t.Errorf("got activation=%.1f after close, want 7.0", activation)
	}
}

func TestSendBackpressuresUntilContextCancelled(t *testing.T) {
	graphDB := setupGraphDB(t)
	provider := &blockingProvider{db: graphDB, entered: make(chan struct{}), release: make(chan struct{})}
	ctx := context.Background()
	buf := writebuffer.New(ctx, provider, 1, time.Hour)
	defer func() {
		close(provider.release)
		_ = buf.Close(context.Background())
	}()

	op := writebuffer.WriteOp{Type: writebuffer.OpUpsertNode, ProjectID: "proj1", Payload: writebuffer.NodeUpsert{
		ID: "n", ProjectID: "proj1", Type: "symbol", Label: "n", CanonicalID: "n", SourceClass: "structural", Properties: "{}",
	}}
	if err := buf.Send(ctx, op); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	<-provider.entered // writer is blocked in its first flush
	if err := buf.Send(ctx, op); err != nil {
		t.Fatalf("second Send: %v", err)
	}

	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := buf.Send(deadline, op); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third Send error = %v, want deadline exceeded", err)
	}
}

func TestFlushReturnsStorageFailure(t *testing.T) {
	ctx := context.Background()
	buf := writebuffer.New(ctx, failingProvider{}, 1, time.Hour)
	defer buf.Close(ctx)

	op := writebuffer.WriteOp{Type: writebuffer.OpUpsertNode, ProjectID: "proj1", Payload: writebuffer.NodeUpsert{
		ID: "n", ProjectID: "proj1", Type: "symbol", Label: "n", CanonicalID: "n", SourceClass: "structural", Properties: "{}",
	}}
	if err := buf.Send(ctx, op); err != nil {
		t.Fatalf("Send: %v", err)
	}
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := buf.Flush(deadline); err == nil || !strings.Contains(err.Error(), "graph unavailable") {
		t.Fatalf("Flush error = %v, want graph failure", err)
	}
}
