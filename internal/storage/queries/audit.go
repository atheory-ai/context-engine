package queries

import (
	"context"
	"database/sql"
	"fmt"
)

// AuditEntry is a row from the audit_entries table in audit.db.
// Append-only — never updated or deleted.
type AuditEntry struct {
	ID           string
	SessionID    string
	TurnID       sql.NullString
	ActorID      string
	TokenID      sql.NullString
	OnBehalfOf   sql.NullString
	Surface      string
	Action       string
	ProjectIDs   sql.NullString // JSON array
	Scope        sql.NullString
	Status       string
	ErrorMessage sql.NullString
	Timestamp    int64
	Properties   string
}

// InsertAuditEntry appends an audit entry. Never updates existing rows.
func InsertAuditEntry(ctx context.Context, db *sql.DB, e AuditEntry) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO audit_entries
			(id, session_id, turn_id, actor_id, token_id, on_behalf_of,
			 surface, action, project_ids, scope, status, error_message, timestamp, properties)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.SessionID, e.TurnID, e.ActorID, e.TokenID, e.OnBehalfOf,
		e.Surface, e.Action, e.ProjectIDs, e.Scope, e.Status, e.ErrorMessage, e.Timestamp, e.Properties)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}

// GetAuditEntriesBySession returns all audit entries for a session, ordered by timestamp.
func GetAuditEntriesBySession(ctx context.Context, db *sql.DB, sessionID string) ([]AuditEntry, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, session_id, turn_id, actor_id, token_id, on_behalf_of,
		       surface, action, project_ids, scope, status, error_message, timestamp, properties
		FROM audit_entries
		WHERE session_id = ?
		ORDER BY timestamp
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get audit entries by session: %w", err)
	}
	defer rows.Close()
	return scanAuditEntries(rows)
}

// GetAuditEntriesByActor returns audit entries for an actor within a time range.
func GetAuditEntriesByActor(ctx context.Context, db *sql.DB, actorID string, fromMS, toMS int64) ([]AuditEntry, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, session_id, turn_id, actor_id, token_id, on_behalf_of,
		       surface, action, project_ids, scope, status, error_message, timestamp, properties
		FROM audit_entries
		WHERE actor_id = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp
	`, actorID, fromMS, toMS)
	if err != nil {
		return nil, fmt.Errorf("get audit entries by actor: %w", err)
	}
	defer rows.Close()
	return scanAuditEntries(rows)
}

func scanAuditEntries(rows *sql.Rows) ([]AuditEntry, error) {
	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.TurnID, &e.ActorID, &e.TokenID, &e.OnBehalfOf,
			&e.Surface, &e.Action, &e.ProjectIDs, &e.Scope, &e.Status,
			&e.ErrorMessage, &e.Timestamp, &e.Properties,
		); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
