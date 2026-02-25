package queries

import (
	"context"
	"database/sql"
	"fmt"
)

// Enrichment is a substrate change record from the Reviewer.
type Enrichment struct {
	ID          string
	RunID       string
	TurnID      string
	LoopIndex   int
	EntityType  string
	EntityID    string
	Action      string
	BeforeState sql.NullString
	AfterState  string
	Rationale   sql.NullString
	CreatedAt   int64
}

// InsertEnrichment writes an enrichment record directly (bypassing the write buffer).
// Prefer the write buffer for high-frequency writes; use this only for migrations
// or administrative tooling.
func InsertEnrichment(ctx context.Context, db *sql.DB, e Enrichment) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO enrichments
			(id, run_id, turn_id, loop_index, entity_type, entity_id, action, before_state, after_state, rationale, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.RunID, e.TurnID, e.LoopIndex, e.EntityType, e.EntityID,
		e.Action, e.BeforeState, e.AfterState, e.Rationale, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert enrichment: %w", err)
	}
	return nil
}

// GetEnrichmentsByRun returns all enrichment records for a run, in loop order.
func GetEnrichmentsByRun(ctx context.Context, db *sql.DB, runID string) ([]Enrichment, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, turn_id, loop_index, entity_type, entity_id, action,
		       before_state, after_state, rationale, created_at
		FROM enrichments
		WHERE run_id = ?
		ORDER BY loop_index, created_at
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("get enrichments by run: %w", err)
	}
	defer rows.Close()
	return scanEnrichments(rows)
}

// GetEnrichmentsByEntity returns all enrichment records for a specific entity.
func GetEnrichmentsByEntity(ctx context.Context, db *sql.DB, entityID string) ([]Enrichment, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, turn_id, loop_index, entity_type, entity_id, action,
		       before_state, after_state, rationale, created_at
		FROM enrichments
		WHERE entity_id = ?
		ORDER BY created_at
	`, entityID)
	if err != nil {
		return nil, fmt.Errorf("get enrichments by entity: %w", err)
	}
	defer rows.Close()
	return scanEnrichments(rows)
}

func scanEnrichments(rows *sql.Rows) ([]Enrichment, error) {
	var enrichments []Enrichment
	for rows.Next() {
		var e Enrichment
		if err := rows.Scan(
			&e.ID, &e.RunID, &e.TurnID, &e.LoopIndex, &e.EntityType, &e.EntityID,
			&e.Action, &e.BeforeState, &e.AfterState, &e.Rationale, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan enrichment: %w", err)
		}
		enrichments = append(enrichments, e)
	}
	return enrichments, rows.Err()
}
