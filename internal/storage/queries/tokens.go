package queries

import (
	"context"
	"database/sql"
	"fmt"
)

// Token is a row from the tokens table in meta.db.
type Token struct {
	ID         string
	Name       string
	Scope      string
	CreatedAt  int64
	ExpiresAt  sql.NullInt64
	LastUsed   sql.NullInt64
	Revoked    int // 0 = active, 1 = revoked
	RevokedAt  sql.NullInt64
	Properties string
}

// InsertToken creates a new token record.
func InsertToken(ctx context.Context, db *sql.DB, t Token) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO tokens (id, name, scope, created_at, expires_at, last_used, revoked, revoked_at, properties)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, t.ID, t.Name, t.Scope, t.CreatedAt, t.ExpiresAt, t.LastUsed, t.Revoked, t.RevokedAt, t.Properties)
	if err != nil {
		return fmt.Errorf("insert token: %w", err)
	}
	return nil
}

// GetToken retrieves a token by its ID.
// Returns nil if not found.
func GetToken(ctx context.Context, db *sql.DB, id string) (*Token, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, scope, created_at, expires_at, last_used, revoked, revoked_at, properties
		FROM tokens WHERE id = ?
	`, id)
	return scanTokenRow(row)
}

// ListTokens returns all tokens, ordered by created_at descending.
func ListTokens(ctx context.Context, db *sql.DB) ([]Token, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, scope, created_at, expires_at, last_used, revoked, revoked_at, properties
		FROM tokens
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.Name, &t.Scope, &t.CreatedAt, &t.ExpiresAt,
			&t.LastUsed, &t.Revoked, &t.RevokedAt, &t.Properties); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// RevokeToken marks a token as revoked.
func RevokeToken(ctx context.Context, db *sql.DB, id string, revokedAt int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE tokens SET revoked = 1, revoked_at = ? WHERE id = ?`, revokedAt, id)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

// TouchToken updates the last_used timestamp for a token.
func TouchToken(ctx context.Context, db *sql.DB, id string, lastUsed int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE tokens SET last_used = ? WHERE id = ?`, lastUsed, id)
	if err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}

func scanTokenRow(row *sql.Row) (*Token, error) {
	var t Token
	err := row.Scan(&t.ID, &t.Name, &t.Scope, &t.CreatedAt, &t.ExpiresAt,
		&t.LastUsed, &t.Revoked, &t.RevokedAt, &t.Properties)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan token: %w", err)
	}
	return &t, nil
}
