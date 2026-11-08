package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"
)

type User struct {
	ID       int64
	Username string
	Email    string
	IsAdmin  bool
}

type NewUser struct {
	Username string
	Email    string
	Password string
	IsAdmin  bool
}

var (
	ErrInvalidUsername = errors.New("username must be 3-32 characters: letters, digits, '.', '_' or '-'")
	ErrInvalidEmail    = errors.New("invalid email address")
	ErrUserExists      = errors.New("username or email already in use")
	ErrUserNotFound    = errors.New("user not found")
)

// ValidateUsername allows no '@', so a login containing '@' is always an email.
func ValidateUsername(s string) error {
	if len(s) < 3 || len(s) > 32 {
		return ErrInvalidUsername
	}
	for _, c := range []byte(s) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '.', c == '_', c == '-':
		default:
			return ErrInvalidUsername
		}
	}
	return nil
}

func ValidateEmail(s string) error {
	// ParseAddress also accepts `Name <a@b>`; requiring the bare address rejects that.
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || len(s) > 254 {
		return ErrInvalidEmail
	}
	return nil
}

// CreateUser validates nu, hashes the password and inserts the user.
// Username and email are trimmed first.
func CreateUser(ctx context.Context, db *sql.DB, nu NewUser) (int64, error) {
	nu.Username = strings.TrimSpace(nu.Username)
	nu.Email = strings.TrimSpace(nu.Email)
	if err := ValidateUsername(nu.Username); err != nil {
		return 0, err
	}
	if err := ValidateEmail(nu.Email); err != nil {
		return 0, err
	}
	hash, err := HashPassword(ctx, nu.Password)
	if err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// The columns are COLLATE NOCASE, so this check is case-insensitive too.
	// The UNIQUE constraints stay the last line of defence.
	var exists bool
	err = tx.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM users WHERE username = ? OR email = ?)", nu.Username, nu.Email).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if exists {
		return 0, ErrUserExists
	}
	res, err := tx.ExecContext(ctx,
		"INSERT INTO users (username, email, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?)",
		nu.Username, nu.Email, hash, nu.IsAdmin, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// FindForLogin looks a user up by email (if login contains '@') or username,
// and returns the user with their password hash.
func FindForLogin(ctx context.Context, db *sql.DB, login string) (User, string, error) {
	q := "SELECT id, username, email, is_admin, password_hash FROM users WHERE username = ?"
	if strings.Contains(login, "@") {
		q = "SELECT id, username, email, is_admin, password_hash FROM users WHERE email = ?"
	}
	var u User
	var hash string
	err := db.QueryRowContext(ctx, q, login).Scan(&u.ID, &u.Username, &u.Email, &u.IsAdmin, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrUserNotFound
	}
	return u, hash, err
}

// PasswordHash returns the stored hash of a user.
func PasswordHash(ctx context.Context, db *sql.DB, userID int64) (string, error) {
	var hash string
	err := db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id = ?", userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUserNotFound
	}
	return hash, err
}

// GetUser returns the user with id.
func GetUser(ctx context.Context, db *sql.DB, id int64) (User, error) {
	var u User
	err := db.QueryRowContext(ctx,
		"SELECT id, username, email, is_admin FROM users WHERE id = ?", id).Scan(&u.ID, &u.Username, &u.Email, &u.IsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

// ListUsers returns every user, ordered by username.
func ListUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, username, email, is_admin FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.IsAdmin); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// RegistrationOpen reports whether new users may register; closed unless an
// admin opened it.
func RegistrationOpen(ctx context.Context, db *sql.DB) (bool, error) {
	var v string
	err := db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'registration_open'").Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == "1", err
}

func SetRegistrationOpen(ctx context.Context, db *sql.DB, open bool) error {
	v := "0"
	if open {
		v = "1"
	}
	_, err := db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES ('registration_open', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", v)
	return err
}
