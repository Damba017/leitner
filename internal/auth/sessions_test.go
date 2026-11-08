package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func sessionCount(t *testing.T, db *sql.DB, userID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sessions WHERE user_id = ?", userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	id := mustCreate(t, db, "anna", false)
	start := time.Unix(1_800_000_000, 0)

	token, err := NewSession(ctx, db, id, start)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != tokenLen {
		t.Fatalf("token length = %d, want %d", len(token), tokenLen)
	}
	var stored int
	if err := db.QueryRow("SELECT count(*) FROM sessions WHERE token_hash = ?", token).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Error("the plain token is stored in the database")
	}

	tests := []struct {
		name         string
		at           time.Time
		wantErr      error
		wantExtended bool
	}{
		{"right away", start, nil, false},
		{"within a day: no write", start.Add(23 * time.Hour), nil, false},
		{"after a day: extended", start.Add(25 * time.Hour), nil, true},
		{"just after: not again", start.Add(26 * time.Hour), nil, false},
		// Extended at 25 h, so it now lasts until 25 h + TTL.
		{"past the first expiry", start.Add(SessionTTL + time.Hour), nil, true},
		{"unused for a whole TTL", start.Add(2*SessionTTL + 2*time.Hour), ErrNoSession, false},
		{"expired session is gone", start, ErrNoSession, false},
	}
	for _, tt := range tests {
		u, extended, err := LookupSession(ctx, db, token, tt.at)
		if err != tt.wantErr {
			t.Fatalf("%s: err = %v, want %v", tt.name, err, tt.wantErr)
		}
		if extended != tt.wantExtended {
			t.Errorf("%s: extended = %v, want %v", tt.name, extended, tt.wantExtended)
		}
		if err == nil && (u.ID != id || u.Username != "anna") {
			t.Errorf("%s: user = %+v", tt.name, u)
		}
	}
	if n := sessionCount(t, db, id); n != 0 {
		t.Errorf("expired session not deleted: %d left", n)
	}
}

func TestLookupSessionBadTokens(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	id := mustCreate(t, db, "anna", false)
	now := time.Now()
	token, err := NewSession(ctx, db, id, now)
	if err != nil {
		t.Fatal(err)
	}
	other := []byte(token)
	other[0] ^= 1
	for _, tok := range []string{"", "short", token + "x", string(other)} {
		if _, _, err := LookupSession(ctx, db, tok, now); err != ErrNoSession {
			t.Errorf("%q: err = %v, want ErrNoSession", tok, err)
		}
	}
}

func TestDeleteSessions(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	anna := mustCreate(t, db, "anna", false)
	bert := mustCreate(t, db, "bert", false)
	now := time.Now()
	newSession := func(id int64) string {
		tok, err := NewSession(ctx, db, id, now)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	a1, a2, b1 := newSession(anna), newSession(anna), newSession(bert)

	if err := DeleteSession(ctx, db, a1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LookupSession(ctx, db, a1, now); err != ErrNoSession {
		t.Errorf("deleted session still works: %v", err)
	}
	if _, _, err := LookupSession(ctx, db, a2, now); err != nil {
		t.Errorf("other session of the same user gone: %v", err)
	}

	newSession(anna)
	if err := DeleteUserSessions(ctx, db, anna); err != nil {
		t.Fatal(err)
	}
	if n := sessionCount(t, db, anna); n != 0 {
		t.Errorf("anna has %d sessions left", n)
	}
	if _, _, err := LookupSession(ctx, db, b1, now); err != nil {
		t.Errorf("bert's session gone: %v", err)
	}

	// Deleting the user takes the sessions along (ON DELETE CASCADE).
	if err := DeleteUser(ctx, db, bert); err != nil {
		t.Fatal(err)
	}
	if n := sessionCount(t, db, bert); n != 0 {
		t.Errorf("deleted user has %d sessions left", n)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	id := mustCreate(t, db, "anna", false)
	old := time.Unix(1_800_000_000, 0)
	if _, err := NewSession(ctx, db, id, old); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewSession(ctx, db, id, old.Add(SessionTTL))
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteExpiredSessions(ctx, db, old.Add(SessionTTL)); err != nil {
		t.Fatal(err)
	}
	if n := sessionCount(t, db, id); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
	if _, _, err := LookupSession(ctx, db, fresh, old.Add(SessionTTL)); err != nil {
		t.Errorf("fresh session gone: %v", err)
	}
}
