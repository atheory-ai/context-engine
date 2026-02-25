// Package db manages SQLite database connections for the Context Engine.
// All databases open with WAL mode and standard pragmas applied.
// Uses modernc.org/sqlite — pure Go, no CGO required.
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens a SQLite database for read-write access with the standard CE pragmas.
// path should be the absolute path to the .db file.
// For in-memory databases (tests), use ":memory:".
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// SQLite performs best with a single writer.
	// Multiple readers are fine with WAL mode.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply pragmas %s: %w", path, err)
	}
	return db, nil
}

// OpenReadOnly opens a database for read-only access.
// Multiple read-only connections are safe with WAL mode.
func OpenReadOnly(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open readonly %s: %w", path, err)
	}
	db.SetMaxOpenConns(10) // readers can be concurrent
	db.SetMaxIdleConns(5)
	if err := applyReadPragmas(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply pragmas %s: %w", path, err)
	}
	return db, nil
}

// applyPragmas sets the standard CE pragmas on a read-write connection.
// These are applied on every connection open, before any queries.
func applyPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("exec %q: %w", p, err)
		}
	}
	return nil
}

// applyReadPragmas sets the standard CE pragmas on a read-only connection.
func applyReadPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("exec %q: %w", p, err)
		}
	}
	return nil
}
