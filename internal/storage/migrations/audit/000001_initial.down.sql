DROP INDEX IF EXISTS idx_audit_timestamp;
DROP INDEX IF EXISTS idx_audit_action;
DROP INDEX IF EXISTS idx_audit_actor;
DROP INDEX IF EXISTS idx_audit_turn;
DROP INDEX IF EXISTS idx_audit_session;
DROP TABLE IF EXISTS audit_entries;

DROP INDEX IF EXISTS idx_turns_status;
DROP INDEX IF EXISTS idx_turns_started;
DROP INDEX IF EXISTS idx_turns_session;
DROP TABLE IF EXISTS turns;

DROP INDEX IF EXISTS idx_sessions_started;
DROP INDEX IF EXISTS idx_sessions_surface;
DROP INDEX IF EXISTS idx_sessions_actor;
DROP TABLE IF EXISTS sessions;

DROP TABLE IF EXISTS schema_version;
