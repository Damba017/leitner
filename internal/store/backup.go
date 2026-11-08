package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
)

// Backup writes a consistent copy of the database at src to the new file dst
// (VACUUM INTO), also while the server is using src. src is opened read-only,
// so it is never created or migrated.
func Backup(ctx context.Context, src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	// VACUUM INTO writes into an existing empty file, so dst is made here:
	// O_EXCL refuses any existing path, and 0600 keeps the copy as private as
	// the database whatever the caller's umask.
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("backup: %s already exists", dst)
	} else if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	f.Close()

	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", "file:"+src+"?"+q.Encode())
	if err != nil {
		os.Remove(dst)
		return fmt.Errorf("backup: open %s: %w", src, err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		// Don't leave a half-written copy that looks like a backup.
		os.Remove(dst)
		return fmt.Errorf("backup %s: %w", src, err)
	}

	// The copy inherits WAL mode; a rollback journal keeps it one file that
	// also opens read-only. The server switches it back to WAL when restored.
	cp, err := sql.Open("sqlite", "file:"+dst)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", dst, err)
	}
	defer cp.Close()
	if _, err := cp.ExecContext(ctx, "PRAGMA journal_mode = DELETE"); err != nil {
		return fmt.Errorf("backup %s: %w", dst, err)
	}
	return nil
}
