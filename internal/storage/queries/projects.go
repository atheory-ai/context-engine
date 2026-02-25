package queries

import (
	"context"
	"database/sql"
	"fmt"
)

// Project is a row from the projects table in meta.db.
type Project struct {
	ID            string
	GitURL        string
	Name          string
	Status        string
	GraphPath     sql.NullString
	ConfigHash    sql.NullString
	BasePrompt    sql.NullString
	ArchPrompt    sql.NullString
	CreatedAt     int64
	LastSeenAt    int64
	LastIndexedAt sql.NullInt64
	Properties    string
}

// ProjectPath is a row from the project_paths table.
type ProjectPath struct {
	ProjectID string
	Path      string
	LastSeen  int64
}

// UpsertProject inserts or updates a project record.
func UpsertProject(ctx context.Context, db *sql.DB, p Project) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO projects
			(id, git_url, name, status, graph_path, config_hash, base_prompt, arch_prompt,
			 created_at, last_seen_at, last_indexed_at, properties)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name           = excluded.name,
			status         = excluded.status,
			graph_path     = excluded.graph_path,
			config_hash    = excluded.config_hash,
			base_prompt    = excluded.base_prompt,
			arch_prompt    = excluded.arch_prompt,
			last_seen_at   = excluded.last_seen_at,
			last_indexed_at = excluded.last_indexed_at,
			properties     = excluded.properties
	`, p.ID, p.GitURL, p.Name, p.Status, p.GraphPath, p.ConfigHash,
		p.BasePrompt, p.ArchPrompt, p.CreatedAt, p.LastSeenAt, p.LastIndexedAt, p.Properties)
	if err != nil {
		return fmt.Errorf("upsert project: %w", err)
	}
	return nil
}

// GetProject retrieves a project by its ID.
func GetProject(ctx context.Context, db *sql.DB, id string) (*Project, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, git_url, name, status, graph_path, config_hash, base_prompt, arch_prompt,
		       created_at, last_seen_at, last_indexed_at, properties
		FROM projects WHERE id = ?
	`, id)
	return scanProjectRow(row)
}

// GetProjectByGitURL retrieves a project by its normalized git remote URL.
func GetProjectByGitURL(ctx context.Context, db *sql.DB, gitURL string) (*Project, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, git_url, name, status, graph_path, config_hash, base_prompt, arch_prompt,
		       created_at, last_seen_at, last_indexed_at, properties
		FROM projects WHERE git_url = ?
	`, gitURL)
	return scanProjectRow(row)
}

// GetProjectByPath retrieves a project by a known local filesystem path.
func GetProjectByPath(ctx context.Context, db *sql.DB, path string) (*Project, error) {
	row := db.QueryRowContext(ctx, `
		SELECT p.id, p.git_url, p.name, p.status, p.graph_path, p.config_hash,
		       p.base_prompt, p.arch_prompt, p.created_at, p.last_seen_at,
		       p.last_indexed_at, p.properties
		FROM projects p
		JOIN project_paths pp ON pp.project_id = p.id
		WHERE pp.path = ?
	`, path)
	return scanProjectRow(row)
}

// ListProjects returns all registered projects ordered by last_seen_at descending.
func ListProjects(ctx context.Context, db *sql.DB) ([]Project, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, git_url, name, status, graph_path, config_hash, base_prompt, arch_prompt,
		       created_at, last_seen_at, last_indexed_at, properties
		FROM projects
		ORDER BY last_seen_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.GitURL, &p.Name, &p.Status, &p.GraphPath,
			&p.ConfigHash, &p.BasePrompt, &p.ArchPrompt, &p.CreatedAt,
			&p.LastSeenAt, &p.LastIndexedAt, &p.Properties); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// UpdateProjectStatus updates a project's status field.
func UpdateProjectStatus(ctx context.Context, db *sql.DB, id, status string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE projects SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("update project status: %w", err)
	}
	return nil
}

// UpsertProjectPath registers or refreshes a known filesystem path for a project.
func UpsertProjectPath(ctx context.Context, db *sql.DB, projectID, path string, lastSeen int64) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO project_paths (project_id, path, last_seen)
		VALUES (?, ?, ?)
		ON CONFLICT(project_id, path) DO UPDATE SET last_seen = excluded.last_seen
	`, projectID, path, lastSeen)
	if err != nil {
		return fmt.Errorf("upsert project path: %w", err)
	}
	return nil
}

// UpdateLastIndexedAt sets the last_indexed_at timestamp for a project.
func UpdateLastIndexedAt(ctx context.Context, db *sql.DB, projectID string, ts int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE projects SET last_indexed_at = ? WHERE id = ?`, ts, projectID)
	if err != nil {
		return fmt.Errorf("update last_indexed_at: %w", err)
	}
	return nil
}

// ListProjectPaths returns all known paths for a project.
func ListProjectPaths(ctx context.Context, db *sql.DB, projectID string) ([]ProjectPath, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT project_id, path, last_seen FROM project_paths
		WHERE project_id = ? ORDER BY last_seen DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project paths: %w", err)
	}
	defer rows.Close()

	var paths []ProjectPath
	for rows.Next() {
		var pp ProjectPath
		if err := rows.Scan(&pp.ProjectID, &pp.Path, &pp.LastSeen); err != nil {
			return nil, fmt.Errorf("scan project path: %w", err)
		}
		paths = append(paths, pp)
	}
	return paths, rows.Err()
}

func scanProjectRow(row *sql.Row) (*Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.GitURL, &p.Name, &p.Status, &p.GraphPath,
		&p.ConfigHash, &p.BasePrompt, &p.ArchPrompt, &p.CreatedAt,
		&p.LastSeenAt, &p.LastIndexedAt, &p.Properties)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	return &p, nil
}
