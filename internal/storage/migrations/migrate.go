// Package migrations embeds all SQL migration files and exposes
// RunMeta, RunAudit, RunExecution, and RunGraph functions.
// Migrations run automatically at startup if the schema is behind.
// Uses golang-migrate with iofs source and the pure-Go SQLite driver.
package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed meta/* audit/* execution/* graph/* org/*
var migrationFiles embed.FS

// RunMeta applies pending migrations to meta.db.
func RunMeta(db *sql.DB) error {
	return runMigrations(db, "meta")
}

// RunAudit applies pending migrations to audit.db.
func RunAudit(db *sql.DB) error {
	return runMigrations(db, "audit")
}

// RunExecution applies pending migrations to execution.db.
func RunExecution(db *sql.DB) error {
	return runMigrations(db, "execution")
}

// RunGraph applies pending migrations to a graph database (org.db or <hash>.db).
func RunGraph(db *sql.DB) error {
	return runMigrations(db, "graph")
}

// RunOrg applies org-specific pending migrations to org.db.
// Must be called after RunGraph(db) since it adds tables to the same database.
func RunOrg(db *sql.DB) error {
	return runMigrations(db, "org")
}

func runMigrations(db *sql.DB, name string) error {
	src, err := iofs.New(migrationFiles, name)
	if err != nil {
		return fmt.Errorf("migration source %s: %w", name, err)
	}
	driver, err := sqlite.WithInstance(db, &sqlite.Config{
		MigrationsTable: "schema_migrations_" + name,
	})
	if err != nil {
		return fmt.Errorf("migration driver %s: %w", name, err)
	}
	m, err := migrate.NewWithInstance("iofs", src, name, driver)
	if err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up %s: %w", name, err)
	}
	return nil
}
