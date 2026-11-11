package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

const (
	// SessionTTL is how long an unused session lasts. Browsers cap cookies at
	// 400 days; every use extends the session, so in practice it lasts until
	// logout.
	SessionTTL = 400 * 24 * time.Hour
	// A used session is extended at most once per refreshAfter, so not every
	// request writes to the database.
	refreshAfter = 24 * time.Hour
	tokenLen     = 43 // base64url of 32 random bytes, without padding
)

var ErrNoSession = errors.New("no valid session")

// The database only stores this hash, so a leaked database (or backup)
// contains no usable session tokens.
func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// TokenHash is the stored key of the session with token, for rows that end
// with the session (push subscriptions).
func TokenHash(token string) []byte {
	return hashToken(token)
}

// NewSession creates a session for userID and returns its token for the cookie.
func NewSession(ctx context.Context, db *sql.DB, userID int64, now time.Time) (string, error) {
	b := make([]byte, 32)
	rand.Read(b) // never fails (crypto/rand)
	token := base64.RawURLEncoding.EncodeToString(b)
	_, err := db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, created_at, last_seen, expires_at) VALUES (?, ?, ?, ?, ?)",
		hashToken(token), userID, now.Unix(), now.Unix(), now.Add(SessionTTL).Unix())
	if err != nil {
		return "", err
	}
	return token, nil
}

// LookupSession returns the session's user. extended is true when the
// session's lifetime was renewed and the cookie should be sent again.
func LookupSession(ctx context.Context, db *sql.DB, token string, now time.Time) (u User, extended bool, err error) {
	if len(token) != tokenLen {
		return User{}, false, ErrNoSession
	}
	h := hashToken(token)
	var expires, lastSeen int64
	err = db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.email, u.is_admin, s.expires_at, s.last_seen
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, h).Scan(&u.ID, &u.Username, &u.Email, &u.IsAdmin, &expires, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, ErrNoSession
	}
	if err != nil {
		return User{}, false, err
	}
	if now.Unix() >= expires {
		if _, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", h); err != nil {
			return User{}, false, err
		}
		return User{}, false, ErrNoSession
	}
	if now.Sub(time.Unix(lastSeen, 0)) >= refreshAfter {
		_, err := db.ExecContext(ctx,
			"UPDATE sessions SET last_seen = ?, expires_at = ? WHERE token_hash = ?",
			now.Unix(), now.Add(SessionTTL).Unix(), h)
		if err != nil {
			return User{}, false, err
		}
		extended = true
	}
	return u, extended, nil
}

func DeleteSession(ctx context.Context, db *sql.DB, token string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", hashToken(token))
	return err
}

// DeleteUserSessions logs a user out everywhere.
func DeleteUserSessions(ctx context.Context, db *sql.DB, userID int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

func DeleteExpiredSessions(ctx context.Context, db *sql.DB, now time.Time) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now.Unix())
	return err
}
