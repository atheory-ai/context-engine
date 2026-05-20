package runner

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atheory-ai/context-engine/internal/agent/preflight"
	"github.com/atheory-ai/context-engine/internal/agent/reviewer"
	"github.com/atheory-ai/context-engine/internal/agent/strategizer"
	"github.com/atheory-ai/context-engine/internal/agent/synthesizer"
	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/graph/activation"
	"github.com/atheory-ai/context-engine/internal/graph/substrate"
	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

func TestFullEngineLoopGolden(t *testing.T) {
	fixture := loadFullLoopFixture(t)
	ctx := context.Background()

	harness := newGoldenLoopHarness(t, ctx, fixture)
	ch := core.NewAppChannels()

	if err := harness.dag.RunWithChannels(ctx, fixture.Query, &ch); err != nil {
		t.Fatalf("RunWithChannels(): %v", err)
	}

	if len(harness.tool.requests) != 1 {
		t.Fatalf("fixture tool executions = %d, want 1", len(harness.tool.requests))
	}

	gotActivation := normalizeAnchors(harness.tool.requests[0].Anchors)
	assertGoldenJSON(t, "activation", gotActivation, fixture.Activation)

	gotToolEmissions := normalizeEmissions(harness.tool.results[0].Emissions)
	assertGoldenJSON(t, "tool emissions", gotToolEmissions, fixture.ToolEmissions)

	reviewerEmission := drainSourceEmission(ch.Thinking, "reviewer")
	if strings.TrimSpace(reviewerEmission.Content) != strings.TrimSpace(fixture.ReviewerXML) {
		t.Fatalf("reviewer output mismatch\n--- got ---\n%s\n--- want ---\n%s",
			reviewerEmission.Content, fixture.ReviewerXML)
	}

	finalMessage := drainLastEmission(ch.Message)
	if strings.TrimSpace(finalMessage.Content) != strings.TrimSpace(fixture.FinalSynthesis) {
		t.Fatalf("final synthesis mismatch\n--- got ---\n%s\n--- want ---\n%s",
			finalMessage.Content, fixture.FinalSynthesis)
	}

	harness.llm.assertConsumed(t)
	harness.llm.assertCall(t, 0, fixture.Query, fixture.StrategizerXML)
	harness.llm.assertCall(t, 1, "## This Iteration (tool findings)", fixture.ReviewerXML)
	harness.llm.assertCall(t, 2, "## Tool Findings", fixture.FinalSynthesis)
}

type fullLoopFixture struct {
	Query          string
	StrategizerXML string
	ReviewerXML    string
	FinalSynthesis string
	Activation     []goldenAnchor
	ToolEmissions  []goldenEmission
}

type goldenAnchor struct {
	CanonicalID string  `json:"canonical_id"`
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Activation  float64 `json:"activation"`
}

type goldenEmission struct {
	Channel string `json:"channel"`
	Source  string `json:"source"`
	Content string `json:"content"`
}

type goldenLoopHarness struct {
	dag  *dag
	llm  *scriptedLLM
	tool *fixtureTool
}

func loadFullLoopFixture(t *testing.T) fullLoopFixture {
	t.Helper()
	dir := filepath.Join("testdata", "full_engine_loop")
	fixture := fullLoopFixture{
		Query:          readTextFixture(t, dir, "query.txt"),
		StrategizerXML: readTextFixture(t, dir, "strategizer.xml"),
		ReviewerXML:    readTextFixture(t, dir, "reviewer.xml"),
		FinalSynthesis: readTextFixture(t, dir, "final.md"),
	}
	readJSONFixture(t, filepath.Join(dir, "activation.golden.json"), &fixture.Activation)
	readJSONFixture(t, filepath.Join(dir, "tool_emissions.golden.json"), &fixture.ToolEmissions)
	return fixture
}

func newGoldenLoopHarness(t *testing.T, ctx context.Context, fixture fullLoopFixture) goldenLoopHarness {
	t.Helper()

	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "graphs"), 0755); err != nil {
		t.Fatalf("create graph dir: %v", err)
	}

	registry := db.NewRegistry()
	t.Cleanup(func() {
		if err := registry.CloseAll(); err != nil {
			t.Fatalf("close registry: %v", err)
		}
	})
	openAndMigrateDBs(t, ctx, registry, dataDir)
	seedProject(t, ctx, registry.Meta())
	seedGoldenGraph(t, ctx, registry, dataDir)

	buffer := writebuffer.New(ctx, registry, writebuffer.DefaultBufferSize, time.Hour)
	t.Cleanup(func() {
		if err := buffer.Close(context.Background()); err != nil {
			t.Fatalf("close write buffer: %v", err)
		}
	})

	sub := substrate.NewReadWriter(registry, buffer)
	llm := &scriptedLLM{
		responses: []core.CompletionResponse{
			{Content: fixture.StrategizerXML, TokensIn: 100, TokensOut: 80, Model: "fixture-thinking", FinishReason: "stop"},
			{Content: fixture.ReviewerXML, TokensIn: 80, TokensOut: 40, Model: "fixture-fast", FinishReason: "stop"},
			{Content: fixture.FinalSynthesis, TokensIn: 120, TokensOut: 35, Model: "fixture-standard", FinishReason: "stop"},
		},
	}
	tool := &fixtureTool{
		result: core.ToolResult{
			Emissions: []core.Emission{
				{
					Channel: core.ChanThinking,
					Source:  "fixture-tool",
					Content: "ProcessPayment calls InvoiceWriter with a validated invoice payload.",
				},
			},
		},
	}

	cfg := &config.Config{
		DataDir: dataDir,
		Engine:  config.EngineConfig{MaxLoops: 1, KLimit: 2},
		LLM: config.LLMConfig{Models: map[string]string{
			core.TierFast:     "fixture-fast",
			core.TierStandard: "fixture-standard",
			core.TierThinking: "fixture-thinking",
		}},
	}
	engine := &Engine{
		cfg:        cfg,
		dbRegistry: registry,
		buffer:     buffer,
		substrate:  sub,
		llmRouter:  nil,
	}
	d := &dag{
		preflight:   preflight.New(registry, llm),
		strategizer: strategizer.New(llm, "", "", nil, []core.Tool{tool}),
		activation:  activation.NewNode(sub.Reader),
		fanout:      &fanoutNode{tools: []core.Tool{tool}},
		reviewer:    reviewer.New(llm, sub),
		synthesizer: synthesizer.New(llm),
		engine:      engine,
	}
	return goldenLoopHarness{dag: d, llm: llm, tool: tool}
}

func openAndMigrateDBs(t *testing.T, ctx context.Context, registry *db.Registry, dataDir string) {
	t.Helper()
	if err := registry.OpenMeta(filepath.Join(dataDir, "meta.db")); err != nil {
		t.Fatalf("open meta: %v", err)
	}
	if err := migrations.RunMeta(registry.Meta()); err != nil {
		t.Fatalf("migrate meta: %v", err)
	}
	if err := registry.OpenAudit(filepath.Join(dataDir, "audit.db")); err != nil {
		t.Fatalf("open audit: %v", err)
	}
	if err := migrations.RunAudit(registry.Audit()); err != nil {
		t.Fatalf("migrate audit: %v", err)
	}
	if err := registry.OpenOrgGraph(filepath.Join(dataDir, "graphs", "org.db")); err != nil {
		t.Fatalf("open org graph: %v", err)
	}
	if err := migrations.RunGraph(mustGraphDB(t, registry, "org")); err != nil {
		t.Fatalf("migrate org graph: %v", err)
	}
	if err := migrations.RunOrg(mustGraphDB(t, registry, "org")); err != nil {
		t.Fatalf("migrate org tables: %v", err)
	}
	_ = ctx
}

func seedProject(t *testing.T, ctx context.Context, metaDB *sql.DB) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := queries.UpsertProject(ctx, metaDB, queries.Project{
		ID:            "local",
		GitURL:        "https://example.invalid/context-engine-fixture.git",
		Name:          "context-engine-fixture",
		Status:        "indexed",
		CreatedAt:     now,
		LastSeenAt:    now,
		LastIndexedAt: sql.NullInt64{Int64: now, Valid: true},
		Properties:    "{}",
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

func seedGoldenGraph(t *testing.T, ctx context.Context, registry *db.Registry, dataDir string) {
	t.Helper()
	graphPath := filepath.Join(dataDir, "graphs", "local.db")
	if err := registry.Mount("local", graphPath); err != nil {
		t.Fatalf("mount local graph: %v", err)
	}
	graphDB := mustGraphDB(t, registry, "local")
	if err := migrations.RunGraph(graphDB); err != nil {
		t.Fatalf("migrate local graph: %v", err)
	}

	now := time.Now().UnixMilli()
	processID := core.MakeNodeID("local", core.NodeTypeSymbol, "internal/billing:ProcessPayment")
	writerID := core.MakeNodeID("local", core.NodeTypeSymbol, "internal/billing:InvoiceWriter")
	edgeID := core.MakeEdgeID(processID, core.EdgeTypeCalls, writerID)

	insertNode(t, ctx, graphDB, processID, "ProcessPayment", "internal/billing:ProcessPayment", now)
	insertNode(t, ctx, graphDB, writerID, "InvoiceWriter", "internal/billing:InvoiceWriter", now)
	insertEdge(t, ctx, graphDB, edgeID, processID, writerID, now)
}

func insertNode(t *testing.T, ctx context.Context, graphDB *sql.DB, id, label, canonicalID string, now int64) {
	t.Helper()
	if _, err := graphDB.ExecContext(ctx, `
		INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, plugin_id, created_at, updated_at, properties)
		VALUES (?, 'local', 'symbol', ?, ?, 'structural', 'fixture', ?, ?, '{}')
	`, id, label, canonicalID, now, now); err != nil {
		t.Fatalf("insert node %s: %v", canonicalID, err)
	}
}

func insertEdge(t *testing.T, ctx context.Context, graphDB *sql.DB, id, sourceID, targetID string, now int64) {
	t.Helper()
	if _, err := graphDB.ExecContext(ctx, `
		INSERT INTO edges (id, project_id, source_id, target_id, type, source_class, plugin_id, created_at, properties)
		VALUES (?, 'local', ?, ?, 'calls', 'structural', 'fixture', ?, '{}')
	`, id, sourceID, targetID, now); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if _, err := graphDB.ExecContext(ctx, `
		INSERT INTO edge_weight (edge_id, weight, source_class, co_activation_count, updated_at)
		VALUES (?, 0.8, 'structural', 0, ?)
	`, id, now); err != nil {
		t.Fatalf("insert edge weight: %v", err)
	}
}

func mustGraphDB(t *testing.T, registry *db.Registry, projectID string) *sql.DB {
	t.Helper()
	graphDB, err := registry.GraphDB(projectID)
	if err != nil {
		t.Fatalf("graph db %s: %v", projectID, err)
	}
	return graphDB
}

type scriptedLLM struct {
	mu        sync.Mutex
	responses []core.CompletionResponse
	requests  []core.CompletionRequest
}

func (s *scriptedLLM) Complete(_ context.Context, req core.CompletionRequest) (core.CompletionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) >= len(s.responses) {
		return core.CompletionResponse{}, fmt.Errorf("unexpected LLM call %d", len(s.requests)+1)
	}
	s.requests = append(s.requests, req)
	return s.responses[len(s.requests)-1], nil
}

func (s *scriptedLLM) Stream(context.Context, core.CompletionRequest, chan<- string) error {
	return fmt.Errorf("stream not implemented in golden fixture")
}

func (s *scriptedLLM) ModelInfo() core.ModelInfo {
	return core.ModelInfo{ID: "fixture-model", ContextLimit: 8192, Tier: core.TierStandard}
}

func (s *scriptedLLM) EstimateTokens(text string) int {
	return len(strings.Fields(text))
}

func (s *scriptedLLM) ModelForTier(tier string) string {
	return "fixture-" + tier
}

func (s *scriptedLLM) assertConsumed(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) != len(s.responses) {
		t.Fatalf("LLM calls = %d, want %d", len(s.requests), len(s.responses))
	}
}

func (s *scriptedLLM) assertCall(t *testing.T, idx int, promptContains string, response string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx >= len(s.requests) {
		t.Fatalf("missing LLM call %d", idx)
	}
	req := s.requests[idx]
	var prompt string
	if len(req.Messages) > 0 {
		prompt = req.Messages[0].Content
	}
	if !strings.Contains(prompt, promptContains) {
		t.Fatalf("LLM call %d prompt missing %q\nprompt:\n%s", idx, promptContains, prompt)
	}
	if s.responses[idx].Content != response {
		t.Fatalf("LLM call %d response fixture mismatch", idx)
	}
}

type fixtureTool struct {
	result   core.ToolResult
	requests []core.ToolRequest
	results  []core.ToolResult
}

func (f *fixtureTool) Name() string { return "fixture-tool" }

func (f *fixtureTool) Description() string {
	return "Returns deterministic call-path evidence for the full-loop golden test."
}

func (f *fixtureTool) Activate(ir core.IR) bool {
	return ir.Predicates[f.Name()] == "true"
}

func (f *fixtureTool) Execute(_ context.Context, req core.ToolRequest) (core.ToolResult, error) {
	f.requests = append(f.requests, req)
	f.results = append(f.results, f.result)
	return f.result, nil
}

func normalizeAnchors(anchors []core.Anchor) []goldenAnchor {
	out := make([]goldenAnchor, 0, len(anchors))
	for _, anchor := range anchors {
		if anchor.Node == nil {
			continue
		}
		out = append(out, goldenAnchor{
			CanonicalID: anchor.Node.CanonicalID,
			ID:          string(anchor.Node.ID),
			Type:        anchor.Node.Type,
			Activation:  roundFloat(anchor.Activation, 2),
		})
	}
	return out
}

func normalizeEmissions(emissions []core.Emission) []goldenEmission {
	out := make([]goldenEmission, 0, len(emissions))
	for _, emission := range emissions {
		out = append(out, goldenEmission{
			Channel: string(emission.Channel),
			Source:  emission.Source,
			Content: emission.Content,
		})
	}
	return out
}

func drainSourceEmission(ch <-chan core.Emission, source string) core.Emission {
	for {
		select {
		case emission := <-ch:
			if emission.Source == source {
				return emission
			}
		default:
			return core.Emission{}
		}
	}
}

func drainLastEmission(ch <-chan core.Emission) core.Emission {
	var last core.Emission
	for {
		select {
		case emission := <-ch:
			last = emission
		default:
			return last
		}
	}
}

func assertGoldenJSON[T any](t *testing.T, name string, got, want T) {
	t.Helper()
	gotJSON := mustPrettyJSON(t, got)
	wantJSON := mustPrettyJSON(t, want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, gotJSON, wantJSON)
	}
}

func mustPrettyJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return append(data, '\n')
}

func readTextFixture(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.TrimRight(string(data), "\n")
}

func readJSONFixture(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
}

func roundFloat(v float64, places int) float64 {
	scale := 1.0
	for i := 0; i < places; i++ {
		scale *= 10
	}
	if v < 0 {
		return float64(int(v*scale-0.5)) / scale
	}
	return float64(int(v*scale+0.5)) / scale
}
