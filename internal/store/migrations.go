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

	// 3: the tree of headings and lists. A NULL parent_id is the top level.
	// Sub-headings and lists inside one heading share one position sequence,
	// so the user can mix them. Deleting a heading cascades down the tree.
	// One index per foreign key, so cascades and child lookups don't scan.
	`CREATE TABLE headings (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		parent_id  INTEGER REFERENCES headings(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		position   INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	) STRICT;
	CREATE INDEX headings_user ON headings(user_id);
	CREATE INDEX headings_parent ON headings(parent_id);
	CREATE TABLE lists (
		id             INTEGER PRIMARY KEY,
		user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		heading_id     INTEGER NOT NULL REFERENCES headings(id) ON DELETE CASCADE,
		name           TEXT NOT NULL,
		position       INTEGER NOT NULL,
		lang1          TEXT NOT NULL,
		lang2          TEXT NOT NULL,
		direction      TEXT NOT NULL CHECK (direction IN ('1-2', '2-1', 'both')),
		strict_case    INTEGER NOT NULL DEFAULT 0 CHECK (strict_case IN (0, 1)),
		strict_punct   INTEGER NOT NULL DEFAULT 0 CHECK (strict_punct IN (0, 1)),
		strict_accents INTEGER NOT NULL DEFAULT 0 CHECK (strict_accents IN (0, 1)),
		created_at     INTEGER NOT NULL
	) STRICT;
	CREATE INDEX lists_user ON lists(user_id);
	CREATE INDEX lists_heading ON lists(heading_id);`,

	// 4: cards. Several correct answers inside one side are separated by "/"
	// (kept as typed; the answer check splits them in phase 5). Cards keep
	// creation order (id). Deleting a list or user cascades to its cards.
	`CREATE TABLE cards (
		id            INTEGER PRIMARY KEY,
		user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		list_id       INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
		front         TEXT NOT NULL,
		back          TEXT NOT NULL,
		note          TEXT NOT NULL DEFAULT '',
		pronunciation TEXT NOT NULL DEFAULT '',
		created_at    INTEGER NOT NULL
	) STRICT;
	CREATE INDEX cards_user ON cards(user_id);
	CREATE INDEX cards_list ON cards(list_id);`,
}
