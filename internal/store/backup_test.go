package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "leitner.db")
	db, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('greeting', 'hallo')`); err != nil {
		t.Fatal(err)
	}

	// The server keeps src open, so the new row may still be in the WAL only.
	dst := filepath.Join(dir, "backup.db")
	if err := Backup(t.Context(), src, dst); err != nil {
		t.Fatal(err)
	}
	// One self-contained file that opens read-only.
	if _, err := os.Stat(dst + "-wal"); !os.IsNotExist(err) {
		t.Errorf("backup has a -wal file next to it (stat: %v)", err)
	}
	cp, err := sql.Open("sqlite", "file:"+dst+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	var mode string
	if err := cp.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "delete" {
		t.Errorf("backup journal_mode = %q (%v), want delete", mode, err)
	}
	var value string
	if err := cp.QueryRow("SELECT value FROM settings WHERE key = 'greeting'").Scan(&value); err != nil || value != "hallo" {
		t.Errorf("backup has value %q (%v), want hallo", value, err)
	}
	if v := userVersion(t, cp); v != len(migrations) {
		t.Errorf("backup user_version = %d, want %d", v, len(migrations))
	}
}

// The copy holds the same data as the database, so it gets the same 0600,
// also when the caller's umask is the usual 022 (sudo -u, runuser).
func TestBackupIsPrivate(t *testing.T) {
	defer syscall.Umask(syscall.Umask(0o022))
	dir := t.TempDir()
	src := filepath.Join(dir, "leitner.db")
	db, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	dst := filepath.Join(dir, "backup.db")
	if err := Backup(t.Context(), src, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("backup mode = %#o, want 0600", perm)
	}
}

func TestBackupRefusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "leitner.db")
	db, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	for _, content := range []string{"keep me", ""} {
		dst := filepath.Join(dir, "existing.db")
		if err := os.WriteFile(dst, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Backup(t.Context(), src, dst); err == nil {
			t.Errorf("backup over a file with %q: no error", content)
		}
		if got, _ := os.ReadFile(dst); string(got) != content {
			t.Errorf("existing file changed to %q, want %q", got, content)
		}
	}
}

func TestBackupDoesNotCreateSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "missing.db")
	dst := filepath.Join(dir, "backup.db")
	if err := Backup(t.Context(), src, dst); err == nil {
		t.Error("backup of a missing database: no error")
	}
	for _, p := range []string{src, dst} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s was created (stat: %v)", p, err)
		}
	}
}

func TestBackupDoesNotMigrate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.db")
	db, err := open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// An older schema (version 0 here) must stay as it is, in source and copy.
	dst := filepath.Join(dir, "backup.db")
	if err := Backup(t.Context(), src, dst); err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, db); v != 0 {
		t.Errorf("source user_version = %d after backup, want 0", v)
	}
	cp, err := open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	if v := userVersion(t, cp); v != 0 {
		t.Errorf("backup user_version = %d, want 0", v)
	}
}
