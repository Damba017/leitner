package store

// migrations is append-only: never edit or reorder an entry that has been
// released. Entry i brings the schema to version i+1.
// Times are stored as unix seconds (UTC).
var migrations = []string{
	// 1: app-wide settings (registration switch; later the VAPID key).
	`CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	) STRICT;`,

	// 2: accounts and login sessions. Usernames and emails are unique
	// regardless of case. Sessions store only the SHA-256 of their token.
	`CREATE TABLE users (
		id            INTEGER PRIMARY KEY,
		username      TEXT NOT NULL COLLATE NOCASE UNIQUE,
		email         TEXT NOT NULL COLLATE NOCASE UNIQUE,
		password_hash TEXT NOT NULL,
		is_admin      INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0, 1)),
		created_at    INTEGER NOT NULL
	) STRICT;
	CREATE TABLE sessions (
		token_hash BLOB PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at INTEGER NOT NULL,
		last_seen  INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	) STRICT;
	CREATE INDEX sessions_user ON sessions(user_id);`,
}
