package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func countTables(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type = 'table'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var twoMigrations = []string{
	"CREATE TABLE a (id INTEGER PRIMARY KEY)",
	"CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER REFERENCES a(id))",
}

func TestMigrate(t *testing.T) {
	tests := []struct {
		name        string
		first       []string // applied before the step under test; nil = skip
		migrations  []string
		wantErr     bool
		wantVersion int
		wantTables  int
	}{
		{"fresh", nil, twoMigrations, false, 2, 2},
		{"rerun is a no-op", twoMigrations, twoMigrations, false, 2, 2},
		{"only new ones", twoMigrations[:1], twoMigrations, false, 2, 2},
		{"empty list", nil, []string{}, false, 0, 0},
		{"broken migration rolls back", twoMigrations[:1],
			append(twoMigrations[:1:1], "CREATE TABLE c (id INTEGER PRIMARY KEY); CREATE TABLE oops ("), true, 1, 1},
		{"database newer than binary", twoMigrations, twoMigrations[:1], true, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tempDB(t)
			if tt.first != nil {
				if err := Migrate(db, tt.first); err != nil {
					t.Fatal(err)
				}
			}
			err := Migrate(db, tt.migrations)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if v := userVersion(t, db); v != tt.wantVersion {
				t.Errorf("user_version = %d, want %d", v, tt.wantVersion)
			}
			if n := countTables(t, db); n != tt.wantTables {
				t.Errorf("tables = %d, want %d", n, tt.wantTables)
			}
		})
	}
}

func TestPragmas(t *testing.T) {
	db := tempDB(t)
	// Hold one connection so the queries below need a second, fresh one:
	// the pragmas must apply to every connection, not just the first.
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	tests := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
		{"synchronous", "1"}, // NORMAL
	}
	for _, tt := range tests {
		var got string
		if err := db.QueryRow("PRAGMA " + tt.pragma).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%s = %q, want %q", tt.pragma, got, tt.want)
		}
	}
}

func TestOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leitner.db")
	for range 2 { // the second Open finds everything in place
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if v := userVersion(t, db); v != len(migrations) {
			t.Errorf("user_version = %d, want %d", v, len(migrations))
		}
		if _, err := db.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES ('k', 'v')"); err != nil {
			t.Errorf("settings table: %v", err)
		}
		db.Close()
	}
}
