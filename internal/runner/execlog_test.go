package runner

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
)

func TestLogLLMCallSkipsReadOnlySessions(t *testing.T) {
	execDB := setupExecutionDB(t)
	ch := core.NewAppChannels()
	engine := &Engine{
		cfg:        &config.Config{Tracing: config.TracingConfig{Enabled: true}, ReadOnly: true},
		channels:   &ch,
		dbRegistry: db.NewRegistry(),
	}
	engine.dbRegistry.SetExec(execDB)

	engine.logLLMCall(testRunContext(), "strategizer", testCompletionRequest(), testCompletionResponse(), nil)
	time.Sleep(20 * time.Millisecond)

	assertExecutionCount(t, execDB, 0)
}

func TestLogLLMCallWritesWhenTracingEnabledAndWritable(t *testing.T) {
	execDB := setupExecutionDB(t)
	ch := core.NewAppChannels()
	engine := &Engine{
		cfg:        &config.Config{Tracing: config.TracingConfig{Enabled: true}},
		channels:   &ch,
		dbRegistry: db.NewRegistry(),
	}
	engine.dbRegistry.SetExec(execDB)

	engine.logLLMCall(testRunContext(), "strategizer", testCompletionRequest(), testCompletionResponse(), nil)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if executionCount(t, execDB) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertExecutionCount(t, execDB, 1)
}

func setupExecutionDB(t *testing.T) *sql.DB {
	t.Helper()
	execDB, err := db.Open(filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatalf("open execution db: %v", err)
	}
	t.Cleanup(func() { execDB.Close() })
	if err := migrations.RunExecution(execDB); err != nil {
		t.Fatalf("migrate execution db: %v", err)
	}
	return execDB
}

func testRunContext() *core.RunContext {
	return &core.RunContext{
		Ctx:       context.Background(),
		RunID:     "run-exec",
		TurnID:    "turn-exec",
		SessionID: "session-exec",
		ProjectID: "project-exec",
		Budget:    core.NewBudget(1000),
	}
}

func testCompletionRequest() core.CompletionRequest {
	return core.CompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []core.Message{
			{Role: "user", Content: "test"},
		},
	}
}

func testCompletionResponse() core.CompletionResponse {
	return core.CompletionResponse{
		Content:   "response",
		TokensIn:  10,
		TokensOut: 5,
		Model:     "claude-sonnet-4-6",
	}
}

func assertExecutionCount(t *testing.T, execDB *sql.DB, want int) {
	t.Helper()
	got := executionCount(t, execDB)
	if got != want {
		t.Fatalf("execution log count = %d, want %d", got, want)
	}
}

func executionCount(t *testing.T, execDB *sql.DB) int {
	t.Helper()
	var count int
	if err := execDB.QueryRow(`SELECT COUNT(*) FROM execution_log`).Scan(&count); err != nil {
		t.Fatalf("count execution logs: %v", err)
	}
	return count
}
