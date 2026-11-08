package auth

import (
	"context"
	"database/sql"
	"errors"
)

var ErrLastAdmin = errors.New("cannot remove the last admin")

// checkNotLastAdmin fails with ErrLastAdmin if userID is the only admin, and
// with ErrUserNotFound if the user doesn't exist. Call it inside the
// transaction that changes or deletes the user.
func checkNotLastAdmin(ctx context.Context, tx *sql.Tx, userID int64) error {
	var isAdmin bool
	err := tx.QueryRowContext(ctx, "SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil || !isAdmin {
		return err
	}
	var admins int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE is_admin = 1").Scan(&admins); err != nil {
		return err
	}
	if admins <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// SetAdmin grants or revokes admin rights. Revoking fails for the last admin.
// It takes effect on the user's next request: sessions read is_admin each time.
func SetAdmin(ctx context.Context, db *sql.DB, userID int64, admin bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !admin {
		if err := checkNotLastAdmin(ctx, tx, userID); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, "UPDATE users SET is_admin = ? WHERE id = ?", admin, userID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrUserNotFound
	}
	return tx.Commit()
}

// DeleteUser removes a user; their sessions (and later their headings, lists
// and cards) go with them through ON DELETE CASCADE. It fails for the last admin.
func DeleteUser(ctx context.Context, db *sql.DB, userID int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkNotLastAdmin(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPassword replaces a user's password and deletes their sessions, except
// the one with keepToken (empty deletes all of them).
func SetPassword(ctx context.Context, db *sql.DB, userID int64, newPassword, keepToken string) error {
	// Hash before the transaction: it holds the write lock, the hash takes a while.
	hash, err := HashPassword(ctx, newPassword)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", hash, userID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrUserNotFound
	}
	// Without keepToken, keep stays nil (NULL), and IS NOT NULL deletes every session.
	var keep []byte
	if keepToken != "" {
		keep = hashToken(keepToken)
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM sessions WHERE user_id = ? AND token_hash IS NOT ?", userID, keep); err != nil {
		return err
	}
	return tx.Commit()
}
