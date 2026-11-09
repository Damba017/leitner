package library

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxCardText is the longest side, note or pronunciation, in characters.
	// Sentences fit; a whole paragraph doesn't belong on a flash card.
	MaxCardText = 500
	// MaxImportCards bounds one paste or CSV import.
	MaxImportCards = 2000
)

var ErrInvalidCard = errors.New("invalid card")

// CardFields is what the user types on a card. Pronunciation is the text the
// read-aloud button speaks for side 1 instead of the written text.
type CardFields struct {
	Front         string `json:"front"`
	Back          string `json:"back"`
	Note          string `json:"note"`
	Pronunciation string `json:"pronunciation"`
}

// Card is a stored card.
type Card struct {
	ID     int64 `json:"id"`
	ListID int64 `json:"listId"`
	CardFields
}

const cardColumns = `id, list_id, front, back, note, pronunciation`

func scanCard(s scanner) (Card, error) {
	var c Card
	err := s.Scan(&c.ID, &c.ListID, &c.Front, &c.Back, &c.Note, &c.Pronunciation)
	return c, err
}

// cleanText trims s. It must be valid UTF-8 on one line (a card is one line
// in a pasted list) and at most MaxCardText characters.
func cleanText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxCardText {
		return "", false
	}
	if strings.ContainsFunc(s, unicode.IsControl) {
		return "", false
	}
	return s, true
}

// clean trims all fields; both sides are required.
func (f *CardFields) clean() error {
	for _, p := range []*string{&f.Front, &f.Back, &f.Note, &f.Pronunciation} {
		s, ok := cleanText(*p)
		if !ok {
			return ErrInvalidCard
		}
		*p = s
	}
	if f.Front == "" || f.Back == "" {
		return ErrInvalidCard
	}
	return nil
}

// listExists reports whether id is a list of userID.
func listExists(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, userID, id int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM lists WHERE id = ? AND user_id = ?", id, userID).Scan(&n)
	return n > 0, err
}

// ListCards returns the cards of list listID in creation order.
func ListCards(ctx context.Context, db *sql.DB, userID, listID int64) ([]Card, error) {
	if ok, err := listExists(ctx, db, userID, listID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	rows, err := db.QueryContext(ctx, `SELECT `+cardColumns+` FROM cards
		WHERE user_id = ? AND list_id = ? ORDER BY id`, userID, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, c)
	}
	return cards, rows.Err()
}

// CreateCard adds a card to list listID.
func CreateCard(ctx context.Context, db *sql.DB, userID, listID int64, f CardFields) (Card, error) {
	if err := f.clean(); err != nil {
		return Card{}, err
	}
	// One statement checks the list's owner and inserts, so there is no gap
	// between the check and the insert.
	res, err := db.ExecContext(ctx, `INSERT INTO cards (user_id, list_id, front, back, note, pronunciation, created_at)
		SELECT ?, id, ?, ?, ?, ?, ? FROM lists WHERE id = ? AND user_id = ?`,
		userID, f.Front, f.Back, f.Note, f.Pronunciation, time.Now().Unix(), listID, userID)
	if err != nil {
		return Card{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Card{}, err
	} else if n == 0 {
		return Card{}, ErrNotFound
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Card{}, err
	}
	return Card{ID: id, ListID: listID, CardFields: f}, nil
}

// UpdateCard changes the text of card id.
func UpdateCard(ctx context.Context, db *sql.DB, userID, id int64, f CardFields) (Card, error) {
	if err := f.clean(); err != nil {
		return Card{}, err
	}
	var listID int64
	err := db.QueryRowContext(ctx, `UPDATE cards SET front = ?, back = ?, note = ?, pronunciation = ?
		WHERE id = ? AND user_id = ? RETURNING list_id`,
		f.Front, f.Back, f.Note, f.Pronunciation, id, userID).Scan(&listID)
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	if err != nil {
		return Card{}, err
	}
	return Card{ID: id, ListID: listID, CardFields: f}, nil
}

// DeleteCard removes card id.
func DeleteCard(ctx context.Context, db *sql.DB, userID, id int64) error {
	res, err := db.ExecContext(ctx, "DELETE FROM cards WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddCards adds all cards to list listID, or none if any is invalid. It
// returns how many were added.
func AddCards(ctx context.Context, db *sql.DB, userID, listID int64, cards []CardFields) (int, error) {
	if len(cards) > MaxImportCards {
		return 0, ErrTooManyCards
	}
	for i := range cards {
		if err := cards[i].clean(); err != nil {
			return 0, err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if ok, err := listExists(ctx, tx, userID, listID); err != nil {
		return 0, err
	} else if !ok {
		return 0, ErrNotFound
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO cards (user_id, list_id, front, back, note, pronunciation, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, c := range cards {
		if _, err := stmt.ExecContext(ctx, userID, listID, c.Front, c.Back, c.Note, c.Pronunciation, now); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(cards), nil
}
