package handlers

import (
	"net/http"

	"github.com/atheory/context-engine/internal/runner"
)

// LLMCallLog is a single LLM call record for the execution log API.
type LLMCallLog struct {
	CallID       string  `json:"call_id"`
	NodeType     string  `json:"node_type"`
	LoopIndex    int     `json:"loop_index"`
	Model        string  `json:"model"`
	SystemPrompt string  `json:"system_prompt"`
	UserMessage  string  `json:"user_message"`
	Response     string  `json:"response"`
	ThinkingText string  `json:"thinking_text,omitempty"`
	TokensIn     int     `json:"tokens_in"`
	TokensOut    int     `json:"tokens_out"`
	LatencyMS    int64   `json:"latency_ms"`
	CalledAt     int64   `json:"called_at"`
}

// ExecRunDetail is the full execution trace for a single run.
type ExecRunDetail struct {
	RunID       string       `json:"run_id"`
	Query       string       `json:"query"`
	ProjectID   string       `json:"project_id"`
	StartedAt   int64        `json:"started_at"`
	CompletedAt *int64       `json:"completed_at"`
	LoopsUsed   int          `json:"loops_used"`
	TokensIn    int          `json:"tokens_in"`
	TokensOut   int          `json:"tokens_out"`
	CostUSD     float64      `json:"cost_usd"`
	Partial     bool         `json:"partial"`
	LLMCalls    []LLMCallLog `json:"llm_calls"`
}

// ListExecLog handles GET /api/v1/execlog.
func ListExecLog(_ *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"runs":   []any{},
			"total":  0,
			"offset": 0,
		})
	}
}

// GetExecRun handles GET /api/v1/execlog/{runId}.
func GetExecRun(_ *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := r.PathValue("runId")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "runId is required")
			return
		}
		writeError(w, http.StatusNotFound, "run not found")
	}
}
