package runner

import (
	"github.com/atheory/context-engine/internal/core"
)

// logLLMCall writes an execution log entry after every LLM completion.
// No-op in Phase 1 (tracing not yet implemented).
// Phase 2 will write to execution.db when cfg.Tracing.Enabled is true.
func (e *Engine) logLLMCall(
	rc *core.RunContext,
	nodeType string,
	req core.CompletionRequest,
	resp core.CompletionResponse,
	irEmitted *core.IR,
) {
	if !e.cfg.Tracing.Enabled {
		return
	}

	// Phase 1 stub — emit a debug record only.
	rc.Ch.Emit(core.Emission{
		RunID:   rc.RunID,
		TurnID:  rc.TurnID,
		Channel: core.ChanDebug,
		Source:  "execlog",
		Content: nodeType + ": " + resp.Model + " " + resp.FinishReason,
	})
}
