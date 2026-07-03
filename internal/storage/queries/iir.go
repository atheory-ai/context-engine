package queries

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// IIRKind classifies an IIR row: extracted from source, or intended (declared).
const (
	IIRKindExtracted = "extracted"
	IIRKindIntended  = "intended"
)

// IIR is a stored Intermediate Intent Representation for one function node.
// The Payload is the FunctionIntent JSON — this package does not interpret it;
// internal/iir owns the model.
type IIR struct {
	ID         string
	ProjectID  string
	NodeID     string
	Kind       string
	Language   string
	Payload    string // FunctionIntent JSON
	SourceHash string
	RunID      string
	CreatedAt  int64
	UpdatedAt  int64
}

// IIRID returns the deterministic row id for a (project, node, kind) triple, so
// re-extraction upserts the same row rather than colliding with the
// (project_id, node_id, kind) unique constraint. Mirrors core.MakeNodeID.
func IIRID(projectID, nodeID, kind string) string {
	h := sha256.Sum256([]byte(projectID + ":" + nodeID + ":" + kind))
	return hex.EncodeToString(h[:16])
}

const iirColumns = `id, project_id, node_id, kind, language, iir,
	COALESCE(source_hash, ''), COALESCE(run_id, ''), created_at, updated_at`

// GetIIR retrieves the IIR of a given kind for a node, or nil if absent.
func GetIIR(ctx context.Context, db *sql.DB, projectID, nodeID, kind string) (*IIR, error) {
	row := db.QueryRowContext(ctx, `
		SELECT `+iirColumns+`
		FROM iir
		WHERE project_id = ? AND node_id = ? AND kind = ?
	`, projectID, nodeID, kind)
	return scanIIRRow(row)
}

// GetIIRByNode returns every IIR row for a node (both kinds), kind-ordered so
// intended and extracted can be compared.
func GetIIRByNode(ctx context.Context, db *sql.DB, projectID, nodeID string) ([]IIR, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+iirColumns+`
		FROM iir
		WHERE project_id = ? AND node_id = ?
		ORDER BY kind
	`, projectID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get iir by node: %w", err)
	}
	defer rows.Close()
	return scanIIRs(rows)
}

// ListIIRByProject returns all IIR rows of a kind for a project — the basis for
// repo-wide rule/verification queries. Ordered by node id for determinism.
func ListIIRByProject(ctx context.Context, db *sql.DB, projectID, kind string) ([]IIR, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+iirColumns+`
		FROM iir
		WHERE project_id = ? AND kind = ?
		ORDER BY node_id
	`, projectID, kind)
	if err != nil {
		return nil, fmt.Errorf("list iir by project: %w", err)
	}
	defer rows.Close()
	return scanIIRs(rows)
}

// --- scanning ---------------------------------------------------------------

func scanIIRRow(row *sql.Row) (*IIR, error) {
	var r IIR
	err := row.Scan(&r.ID, &r.ProjectID, &r.NodeID, &r.Kind, &r.Language,
		&r.Payload, &r.SourceHash, &r.RunID, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan iir: %w", err)
	}
	return &r, nil
}

func scanIIRs(rows *sql.Rows) ([]IIR, error) {
	out := []IIR{}
	for rows.Next() {
		var r IIR
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.NodeID, &r.Kind, &r.Language,
			&r.Payload, &r.SourceHash, &r.RunID, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan iir: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
