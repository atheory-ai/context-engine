package queries

import (
	"context"
	"database/sql"
	"fmt"
)

// ExecutionLog is a row from the execution_log table in execution.db.
type ExecutionLog struct {
	ID                string
	RunID             string
	TurnID            string
	SessionID         string
	TraceID           sql.NullString
	NodeType          string
	LoopIndex         int
	IsReplay          int
	Model             string
	Tier              string
	PromptMessages    string // JSON array of {role, content}
	Response          string
	ThinkingTrace     sql.NullString
	IREmitted         sql.NullString
	TokensIn          int
	TokensOut         int
	EstimatedCostUSD  float64
	LatencyMS         int
	ContextUsedPct    float64
	NodesCreated      string // JSON array
	EdgesUpdated      string // JSON array
	SourceTransitions string // JSON array
	Timestamp         int64
	Properties        string
}

// InsertExecutionLog appends a verbatim LLM call record.
// NEVER called for read-scoped token sessions.
func InsertExecutionLog(ctx context.Context, db *sql.DB, e ExecutionLog) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO execution_log
			(id, run_id, turn_id, session_id, trace_id, node_type, loop_index, is_replay,
			 model, tier, prompt_messages, response, thinking_trace, ir_emitted,
			 tokens_in, tokens_out, estimated_cost_usd, latency_ms, context_used_pct,
			 nodes_created, edges_updated, source_transitions, timestamp, properties)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.RunID, e.TurnID, e.SessionID, e.TraceID, e.NodeType, e.LoopIndex, e.IsReplay,
		e.Model, e.Tier, e.PromptMessages, e.Response, e.ThinkingTrace, e.IREmitted,
		e.TokensIn, e.TokensOut, e.EstimatedCostUSD, e.LatencyMS, e.ContextUsedPct,
		e.NodesCreated, e.EdgesUpdated, e.SourceTransitions, e.Timestamp, e.Properties)
	if err != nil {
		return fmt.Errorf("insert execution log: %w", err)
	}
	return nil
}

// GetExecutionLogByRun returns all execution log entries for a run, ordered by loop and timestamp.
func GetExecutionLogByRun(ctx context.Context, db *sql.DB, runID string) ([]ExecutionLog, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, turn_id, session_id, trace_id, node_type, loop_index, is_replay,
		       model, tier, prompt_messages, response, thinking_trace, ir_emitted,
		       tokens_in, tokens_out, estimated_cost_usd, latency_ms, context_used_pct,
		       nodes_created, edges_updated, source_transitions, timestamp, properties
		FROM execution_log
		WHERE run_id = ?
		ORDER BY loop_index, timestamp
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("get execution log by run: %w", err)
	}
	defer rows.Close()
	return scanExecutionLogs(rows)
}

// GetExecutionLogBySession returns all execution log entries for a session.
func GetExecutionLogBySession(ctx context.Context, db *sql.DB, sessionID string) ([]ExecutionLog, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, turn_id, session_id, trace_id, node_type, loop_index, is_replay,
		       model, tier, prompt_messages, response, thinking_trace, ir_emitted,
		       tokens_in, tokens_out, estimated_cost_usd, latency_ms, context_used_pct,
		       nodes_created, edges_updated, source_transitions, timestamp, properties
		FROM execution_log
		WHERE session_id = ?
		ORDER BY timestamp
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get execution log by session: %w", err)
	}
	defer rows.Close()
	return scanExecutionLogs(rows)
}

func scanExecutionLogs(rows *sql.Rows) ([]ExecutionLog, error) {
	var logs []ExecutionLog
	for rows.Next() {
		var e ExecutionLog
		if err := rows.Scan(
			&e.ID, &e.RunID, &e.TurnID, &e.SessionID, &e.TraceID,
			&e.NodeType, &e.LoopIndex, &e.IsReplay,
			&e.Model, &e.Tier, &e.PromptMessages, &e.Response,
			&e.ThinkingTrace, &e.IREmitted,
			&e.TokensIn, &e.TokensOut, &e.EstimatedCostUSD, &e.LatencyMS, &e.ContextUsedPct,
			&e.NodesCreated, &e.EdgesUpdated, &e.SourceTransitions,
			&e.Timestamp, &e.Properties,
		); err != nil {
			return nil, fmt.Errorf("scan execution log: %w", err)
		}
		logs = append(logs, e)
	}
	return logs, rows.Err()
}
