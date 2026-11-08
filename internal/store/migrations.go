package store

// migrations is append-only: never edit or reorder an entry that has been
// released. Entry i brings the schema to version i+1.
// Times are stored as unix seconds (UTC).
var migrations = []string{
	// 1: app-wide settings (later: registration switch, VAPID key).
	`CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	) STRICT;`,
}
