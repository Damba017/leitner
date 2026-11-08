// Package store opens the SQLite database and keeps its schema up to date.
package store

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) the database at path and applies all pending migrations.
func Open(path string) (*sql.DB, error) {
	db, err := open(path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(db, migrations); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func open(path string) (*sql.DB, error) {
	// Pragmas in the DSN are applied by the driver to every new connection,
	// not just the first one in the pool. _txlock=immediate takes the write
	// lock at BEGIN, so a transaction never fails halfway with SQLITE_BUSY
	// when it upgrades from reading to writing.
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// Migrate applies the migrations the database doesn't have yet.
// migrations[i] brings the schema to version i+1, tracked in PRAGMA user_version.
// Each migration runs in its own transaction together with the version bump.
func Migrate(db *sql.DB, migrations []string) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this binary (%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("migration %d: begin: %w", i+1, err)
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		// PRAGMA doesn't accept ? parameters; the value is our own integer.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: set version: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d: commit: %w", i+1, err)
		}
	}
	return nil
}
