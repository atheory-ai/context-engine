package runner

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/llm"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

// logLLMCall writes an execution log entry after every LLM completion.
// No-op if tracing is not enabled or if the execution.db is not open.
// Writes are fire-and-forget in a goroutine — non-blocking, non-fatal on failure.
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
	if e.cfg.ReadOnly {
		return
	}
	execDB := e.dbRegistry.Exec()
	if execDB == nil {
		return
	}

	messagesJSON, _ := json.Marshal(req.Messages)

	var irJSON sql.NullString
	if irEmitted != nil {
		b, _ := json.Marshal(irEmitted)
		irJSON = sql.NullString{String: string(b), Valid: true}
	}

	var thinkingTrace sql.NullString
	if resp.ThinkingText != "" {
		thinkingTrace = sql.NullString{String: resp.ThinkingText, Valid: true}
	}

	entry := queries.ExecutionLog{
		ID:                newExecID(),
		RunID:             string(rc.RunID),
		TurnID:            string(rc.TurnID),
		SessionID:         string(rc.SessionID),
		NodeType:          nodeType,
		LoopIndex:         rc.CurrentLoop(),
		Model:             resp.Model,
		Tier:              inferTier(resp.Model),
		PromptMessages:    string(messagesJSON),
		Response:          resp.Content,
		ThinkingTrace:     thinkingTrace,
		IREmitted:         irJSON,
		TokensIn:          resp.TokensIn,
		TokensOut:         resp.TokensOut,
		EstimatedCostUSD:  llm.EstimateCost(resp),
		ContextUsedPct:    rc.Budget.ContextUsedPct(),
		Timestamp:         time.Now().UnixMilli(),
		NodesCreated:      "[]",
		EdgesUpdated:      "[]",
		SourceTransitions: "[]",
		Properties:        "{}",
	}

	go func() {
		if err := queries.InsertExecutionLog(context.Background(), execDB, entry); err != nil {
			e.channels.Emit(core.Emission{
				Channel: core.ChanDebug,
				Content: fmt.Sprintf("exec log write: %v", err),
			})
		}
	}()
}

// inferTier returns the model tier string for a given model ID.
func inferTier(model string) string {
	switch model {
	case "claude-haiku-4-5-20251001", "claude-3-5-haiku-20241022":
		return core.TierFast
	case "claude-opus-4-6", "claude-3-opus-20240229":
		return core.TierThinking
	default:
		return core.TierStandard
	}
}

// newExecID generates a short random hex ID for execution log entries.
func newExecID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) //nolint:errcheck // math/rand.Read never returns a non-nil error
	return hex.EncodeToString(b)
}
