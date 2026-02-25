package queries

import (
	"context"
	"database/sql"
	"fmt"
)

// Session is a row from the sessions table in audit.db.
type Session struct {
	ID           string
	ActorID      string
	TokenID      sql.NullString
	Surface      string
	StartedAt    int64
	LastActiveAt int64
	EndedAt      sql.NullInt64
	Properties   string
}

// Turn is a row from the turns table in audit.db.
type Turn struct {
	ID         string
	SessionID  string
	Query      sql.NullString
	StartedAt  int64
	EndedAt    sql.NullInt64
	LoopCount  sql.NullInt64
	Status     string
	Properties string
}

// InsertSession creates a new session record.
func InsertSession(ctx context.Context, db *sql.DB, s Session) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO sessions (id, actor_id, token_id, surface, started_at, last_active_at, ended_at, properties)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, s.ID, s.ActorID, s.TokenID, s.Surface, s.StartedAt, s.LastActiveAt, s.EndedAt, s.Properties)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetSession retrieves a session by its ID.
func GetSession(ctx context.Context, db *sql.DB, id string) (*Session, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, actor_id, token_id, surface, started_at, last_active_at, ended_at, properties
		FROM sessions WHERE id = ?
	`, id)
	var s Session
	err := row.Scan(&s.ID, &s.ActorID, &s.TokenID, &s.Surface, &s.StartedAt,
		&s.LastActiveAt, &s.EndedAt, &s.Properties)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &s, nil
}

// TouchSession updates the last_active_at timestamp for a session.
func TouchSession(ctx context.Context, db *sql.DB, id string, lastActiveAt int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE sessions SET last_active_at = ? WHERE id = ?`, lastActiveAt, id)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// EndSession records the session end time.
func EndSession(ctx context.Context, db *sql.DB, id string, endedAt int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE sessions SET ended_at = ? WHERE id = ?`, endedAt, id)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

// InsertTurn creates a new turn record.
func InsertTurn(ctx context.Context, db *sql.DB, t Turn) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO turns (id, session_id, query, started_at, ended_at, loop_count, status, properties)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, t.ID, t.SessionID, t.Query, t.StartedAt, t.EndedAt, t.LoopCount, t.Status, t.Properties)
	if err != nil {
		return fmt.Errorf("insert turn: %w", err)
	}
	return nil
}

// GetTurn retrieves a turn by its ID.
func GetTurn(ctx context.Context, db *sql.DB, id string) (*Turn, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, session_id, query, started_at, ended_at, loop_count, status, properties
		FROM turns WHERE id = ?
	`, id)
	var t Turn
	err := row.Scan(&t.ID, &t.SessionID, &t.Query, &t.StartedAt, &t.EndedAt,
		&t.LoopCount, &t.Status, &t.Properties)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get turn: %w", err)
	}
	return &t, nil
}

// UpdateTurn updates turn end time, loop count, and status.
func UpdateTurn(ctx context.Context, db *sql.DB, id string, endedAt sql.NullInt64, loopCount sql.NullInt64, status string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE turns SET ended_at = ?, loop_count = ?, status = ? WHERE id = ?
	`, endedAt, loopCount, status, id)
	if err != nil {
		return fmt.Errorf("update turn: %w", err)
	}
	return nil
}

// ListTurnsBySession returns all turns for a session, ordered by started_at.
func ListTurnsBySession(ctx context.Context, db *sql.DB, sessionID string) ([]Turn, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, session_id, query, started_at, ended_at, loop_count, status, properties
		FROM turns WHERE session_id = ? ORDER BY started_at
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list turns: %w", err)
	}
	defer rows.Close()

	var turns []Turn
	for rows.Next() {
		var t Turn
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Query, &t.StartedAt, &t.EndedAt,
			&t.LoopCount, &t.Status, &t.Properties); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		turns = append(turns, t)
	}
	return turns, rows.Err()
}
