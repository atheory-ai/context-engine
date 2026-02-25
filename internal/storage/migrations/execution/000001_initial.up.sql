CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL,
    applied_at  INTEGER NOT NULL
);

INSERT INTO schema_version (version, applied_at) VALUES (1, unixepoch() * 1000);

-- ============================================================
-- Execution Log
-- Verbatim record of every LLM call.
-- Written in development mode always.
-- Written in production only when --trace flag is active.
-- NEVER written for read-scoped token sessions.
-- ============================================================

CREATE TABLE IF NOT EXISTS execution_log (
    id                  TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL,
    turn_id             TEXT NOT NULL,
    session_id          TEXT NOT NULL,
    trace_id            TEXT,

    node_type           TEXT NOT NULL,
    loop_index          INTEGER NOT NULL DEFAULT 0,
    is_replay           INTEGER NOT NULL DEFAULT 0,

    model               TEXT NOT NULL,
    tier                TEXT NOT NULL,
    prompt_messages     TEXT NOT NULL,
    response            TEXT NOT NULL,
    thinking_trace      TEXT,
    ir_emitted          TEXT,

    tokens_in           INTEGER NOT NULL DEFAULT 0,
    tokens_out          INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd  REAL    NOT NULL DEFAULT 0,
    latency_ms          INTEGER NOT NULL DEFAULT 0,
    context_used_pct    REAL    NOT NULL DEFAULT 0,

    nodes_created       TEXT NOT NULL DEFAULT '[]',
    edges_updated       TEXT NOT NULL DEFAULT '[]',
    source_transitions  TEXT NOT NULL DEFAULT '[]',

    timestamp           INTEGER NOT NULL,
    properties          TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_exec_run_id    ON execution_log(run_id);
CREATE INDEX IF NOT EXISTS idx_exec_turn_id   ON execution_log(turn_id);
CREATE INDEX IF NOT EXISTS idx_exec_session   ON execution_log(session_id);
CREATE INDEX IF NOT EXISTS idx_exec_node_type ON execution_log(node_type);
CREATE INDEX IF NOT EXISTS idx_exec_timestamp ON execution_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_exec_model     ON execution_log(model);
