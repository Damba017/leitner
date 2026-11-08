package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Damba017/leitner/internal/store"
)

func ams(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, amsterdam)
}

func TestNightlyTimes(t *testing.T) {
	tests := []struct {
		name       string
		now        time.Time
		last, next time.Time
	}{
		{"before 03:30", ams(2026, 11, 5, 3, 29), ams(2026, 11, 4, 3, 30), ams(2026, 11, 5, 3, 30)},
		{"at 03:30", ams(2026, 11, 5, 3, 30), ams(2026, 11, 5, 3, 30), ams(2026, 11, 6, 3, 30)},
		{"afternoon", ams(2026, 11, 5, 15, 0), ams(2026, 11, 5, 3, 30), ams(2026, 11, 6, 3, 30)},
		{"month end", ams(2026, 11, 30, 12, 0), ams(2026, 11, 30, 3, 30), ams(2026, 12, 1, 3, 30)},
		{"year end", ams(2026, 12, 31, 12, 0), ams(2026, 12, 31, 3, 30), ams(2027, 1, 1, 3, 30)},
		{"spring forward night", ams(2027, 3, 28, 1, 0), ams(2027, 3, 27, 3, 30), ams(2027, 3, 28, 3, 30)},
		{"after spring forward", ams(2027, 3, 28, 4, 0), ams(2027, 3, 28, 3, 30), ams(2027, 3, 29, 3, 30)},
		{"fall back night", ams(2026, 10, 25, 1, 0), ams(2026, 10, 24, 3, 30), ams(2026, 10, 25, 3, 30)},
		{"after fall back", ams(2026, 10, 25, 4, 0), ams(2026, 10, 25, 3, 30), ams(2026, 10, 26, 3, 30)},
		{"given in UTC", ams(2026, 11, 5, 15, 0).UTC(), ams(2026, 11, 5, 3, 30), ams(2026, 11, 6, 3, 30)},
	}
	for _, tt := range tests {
		if got := lastNightly(tt.now); !got.Equal(tt.last) {
			t.Errorf("%s: lastNightly = %v, want %v", tt.name, got, tt.last)
		}
		got := nextNightly(tt.now)
		if !got.Equal(tt.next) {
			t.Errorf("%s: nextNightly = %v, want %v", tt.name, got, tt.next)
		}
		if got.In(amsterdam).Format("15:04") != "03:30" {
			t.Errorf("%s: nextNightly is not at 03:30: %v", tt.name, got)
		}
	}
}

func TestBackupTime(t *testing.T) {
	good := ams(2026, 11, 5, 3, 30)
	if got, ok := backupTime(backupName(good)); !ok || !got.Equal(good) {
		t.Errorf("round trip = %v, %v", got, ok)
	}
	if name := backupName(good.UTC()); name != "leitner-20261105-033000.db" {
		t.Errorf("backupName = %q, want Amsterdam time", name)
	}
	for _, name := range []string{
		"", "leitner.db", "leitner-20261105-033000", "leitner-20261105-033000.db.tmp",
		"leitner-2026110-033000.db", "leitner-20261305-033000.db", "leitner-20261105-0330.db",
		"other-20261105-033000.db", "leitner-20261105-033000.db-wal", "leitner-+20261105-033000.db",
	} {
		if _, ok := backupTime(name); ok {
			t.Errorf("backupTime(%q) accepted", name)
		}
	}
}

func newTestNightly(t *testing.T) *nightlyBackups {
	t.Helper()
	path := filepath.Join(t.TempDir(), "leitner.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return newNightlyBackups(path)
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNightlyPruneKeepsSevenAndIgnoresOtherFiles(t *testing.T) {
	n := newTestNightly(t)
	if err := os.MkdirAll(n.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := ams(2026, 11, 1, 3, 30)
	for i := range 10 {
		write(t, filepath.Join(n.dir, backupName(day.AddDate(0, 0, i))))
	}
	// update.sh's files live one folder up; unrelated files may sit in the folder.
	update := filepath.Join(filepath.Dir(n.dir), "leitner-20250101-000000.db")
	write(t, update)
	other := filepath.Join(n.dir, "notes.txt")
	write(t, other)
	wal := filepath.Join(n.dir, backupName(day)+"-wal")
	write(t, wal)

	n.prune()

	names := n.files()
	if len(names) != nightlyKeep {
		t.Fatalf("%d backups left, want %d: %v", len(names), nightlyKeep, names)
	}
	if want := backupName(day.AddDate(0, 0, 3)); names[0] != want {
		t.Errorf("oldest kept = %s, want %s", names[0], want)
	}
	for _, p := range []string{update, other, wal} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("prune touched %s: %v", p, err)
		}
	}
}

func TestNightlyBackup(t *testing.T) {
	n := newTestNightly(t)
	if got := n.dir; got != filepath.Join(filepath.Dir(n.dbPath), "backups", "nightly") {
		t.Errorf("dir = %s", got)
	}
	now := ams(2026, 11, 5, 3, 30)
	if err := n.backup(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(n.dir, "leitner-20261105-033000.db")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup file: %v, %v", fi, err)
	}
	// A second backup in the same second would overwrite: store.Backup refuses.
	if err := n.backup(t.Context(), now); err == nil {
		t.Error("backup over an existing file succeeded")
	}
	// A restart finds the backup on disk.
	if t2, ok := newNightlyBackups(n.dbPath).latest(); !ok || !t2.Equal(now) {
		t.Errorf("latest after restart = %v, %v", t2, ok)
	}
}

func TestNightlyBackupOfMissingDatabase(t *testing.T) {
	n := newNightlyBackups(filepath.Join(t.TempDir(), "missing.db"))
	if err := n.backup(t.Context(), ams(2026, 11, 5, 3, 30)); err == nil {
		t.Fatal("backup of a missing database succeeded")
	}
	if len(n.files()) != 0 {
		t.Errorf("a failed backup left files: %v", n.files())
	}
}

// run catches up at start when there is no backup, and stops with its context.
func TestNightlyRunCatchesUp(t *testing.T) {
	n := newTestNightly(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { n.run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(n.files()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(n.files()) != 1 {
		t.Fatalf("no catch-up backup made: %v", n.files())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop")
	}
}

// A backup made after the last 03:30 is recent enough: no second one at start.
func TestNightlyRunSkipsWhenRecent(t *testing.T) {
	n := newTestNightly(t)
	if err := n.backup(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	n.run(ctx)
	if got := n.files(); len(got) != 1 {
		t.Errorf("backups = %v, want just the first", got)
	}
}
