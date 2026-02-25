package db

import (
	"database/sql"
	"fmt"
	"sync"
)

// Registry manages all open database connections.
// It is the DBProvider that the write buffer uses.
// Safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	orgDB    *sql.DB
	projects map[string]*sql.DB // projectID → *sql.DB
	metaDB   *sql.DB
	auditDB  *sql.DB
	execDB   *sql.DB
}

// NewRegistry creates an empty Registry.
// Callers must open individual databases via OpenMeta, OpenAudit, OpenExec,
// and OpenOrg before mounting project graphs.
func NewRegistry() *Registry {
	return &Registry{
		projects: make(map[string]*sql.DB),
	}
}

// SetMeta registers the meta.db connection.
func (r *Registry) SetMeta(db *sql.DB) {
	r.mu.Lock()
	r.metaDB = db
	r.mu.Unlock()
}

// SetAudit registers the audit.db connection.
func (r *Registry) SetAudit(db *sql.DB) {
	r.mu.Lock()
	r.auditDB = db
	r.mu.Unlock()
}

// SetExec registers the execution.db connection.
func (r *Registry) SetExec(db *sql.DB) {
	r.mu.Lock()
	r.execDB = db
	r.mu.Unlock()
}

// SetOrg registers the org graph database connection.
func (r *Registry) SetOrg(db *sql.DB) {
	r.mu.Lock()
	r.orgDB = db
	r.mu.Unlock()
}

// Meta returns the meta.db connection.
func (r *Registry) Meta() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.metaDB
}

// Audit returns the audit.db connection.
func (r *Registry) Audit() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.auditDB
}

// Exec returns the execution.db connection.
func (r *Registry) Exec() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.execDB
}

// Mount opens a project graph database and registers it.
// Safe to call multiple times for the same project (idempotent).
func (r *Registry) Mount(projectID, dbPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.projects[projectID]; ok {
		return nil // already mounted
	}
	db, err := Open(dbPath)
	if err != nil {
		return fmt.Errorf("mount project %s: %w", projectID, err)
	}
	r.projects[projectID] = db
	return nil
}

// Unmount closes and deregisters a project graph database.
func (r *Registry) Unmount(projectID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	db, ok := r.projects[projectID]
	if !ok {
		return nil // not mounted, nothing to do
	}
	delete(r.projects, projectID)
	return db.Close()
}

// GraphDB implements DBProvider.
// Returns the org DB for projectID "org".
// Returns the registered project graph for any other project ID.
func (r *Registry) GraphDB(projectID string) (*sql.DB, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if projectID == "org" {
		if r.orgDB == nil {
			return nil, fmt.Errorf("org graph database not open")
		}
		return r.orgDB, nil
	}
	db, ok := r.projects[projectID]
	if !ok {
		return nil, fmt.Errorf("project graph not mounted: %s", projectID)
	}
	return db, nil
}

// OpenMeta opens meta.db and registers it.
func (r *Registry) OpenMeta(path string) error {
	db, err := Open(path)
	if err != nil {
		return fmt.Errorf("open meta db: %w", err)
	}
	r.SetMeta(db)
	return nil
}

// OpenAudit opens audit.db and registers it.
func (r *Registry) OpenAudit(path string) error {
	db, err := Open(path)
	if err != nil {
		return fmt.Errorf("open audit db: %w", err)
	}
	r.SetAudit(db)
	return nil
}

// OpenExecution opens execution.db and registers it.
func (r *Registry) OpenExecution(path string) error {
	db, err := Open(path)
	if err != nil {
		return fmt.Errorf("open execution db: %w", err)
	}
	r.SetExec(db)
	return nil
}

// OpenOrgGraph opens the org-level graph database and registers it.
func (r *Registry) OpenOrgGraph(path string) error {
	db, err := Open(path)
	if err != nil {
		return fmt.Errorf("open org graph db: %w", err)
	}
	r.SetOrg(db)
	return nil
}

// CloseAll closes all open database connections.
// Alias for Close() — used by the runner's Engine.Close().
func (r *Registry) CloseAll() error {
	return r.Close()
}

// Close closes all open database connections.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for id, db := range r.projects {
		if err := db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close project %s: %w", id, err))
		}
	}
	r.projects = make(map[string]*sql.DB)

	for _, db := range []*sql.DB{r.orgDB, r.metaDB, r.auditDB, r.execDB} {
		if db != nil {
			if err := db.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	r.orgDB = nil
	r.metaDB = nil
	r.auditDB = nil
	r.execDB = nil

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
