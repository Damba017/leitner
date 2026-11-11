package library

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidLang      = errors.New("invalid language code")
	ErrInvalidDirection = errors.New("invalid practice direction")
)

// Practice directions: side 1 → side 2, the reverse, or a random side each time.
const (
	Dir12   = "1-2"
	Dir21   = "2-1"
	DirBoth = "both"
)

// langCode is a BCP 47 tag like "es", "pt-BR" or "zh-Hant": what the Web
// Speech API takes. It is checked for shape only, so any language works.
var langCode = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

const maxLangLen = 35

// ListFields is what the user sets on a list.
type ListFields struct {
	HeadingID int64  `json:"headingId"`
	Name      string `json:"name"`
	Lang1     string `json:"lang1"`
	Lang2     string `json:"lang2"`
	Direction string `json:"direction"`
	// Answer checks: true = strict (a difference is wrong), false = ignored.
	StrictCase    bool `json:"strictCase"`
	StrictPunct   bool `json:"strictPunct"`
	StrictAccents bool `json:"strictAccents"`
}

// List is a stored list.
type List struct {
	ID int64 `json:"id"`
	ListFields
	Position int64 `json:"position"`
	Notify   bool  `json:"notify"` // false: no notifications from its cards
}

const listColumns = `id, heading_id, name, position, lang1, lang2, direction, strict_case, strict_punct, strict_accents, notify`

type scanner interface{ Scan(dest ...any) error }

func scanList(s scanner) (List, error) {
	var l List
	err := s.Scan(&l.ID, &l.HeadingID, &l.Name, &l.Position, &l.Lang1, &l.Lang2, &l.Direction,
		&l.StrictCase, &l.StrictPunct, &l.StrictAccents, &l.Notify)
	return l, err
}

func (f *ListFields) clean() error {
	name, err := cleanName(f.Name)
	if err != nil {
		return err
	}
	f.Name = name
	for _, l := range []*string{&f.Lang1, &f.Lang2} {
		*l = strings.TrimSpace(*l)
		if len(*l) > maxLangLen || !langCode.MatchString(*l) {
			return ErrInvalidLang
		}
	}
	switch f.Direction {
	case Dir12, Dir21, DirBoth:
	default:
		return ErrInvalidDirection
	}
	return nil
}

// CreateList adds a list at the end of its heading.
func CreateList(ctx context.Context, db *sql.DB, userID int64, f ListFields) (List, error) {
	if err := f.clean(); err != nil {
		return List{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return List{}, err
	}
	defer tx.Rollback()
	if ok, err := headingExists(ctx, tx, userID, f.HeadingID); err != nil {
		return List{}, err
	} else if !ok {
		return List{}, ErrParentNotFound
	}
	pos, err := nextPosition(ctx, tx, userID, &f.HeadingID)
	if err != nil {
		return List{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO lists (user_id, heading_id, name, position, lang1, lang2,
			direction, strict_case, strict_punct, strict_accents, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, f.HeadingID, f.Name, pos, f.Lang1, f.Lang2, f.Direction,
		f.StrictCase, f.StrictPunct, f.StrictAccents, time.Now().Unix())
	if err != nil {
		return List{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return List{}, err
	}
	if err := tx.Commit(); err != nil {
		return List{}, err
	}
	return List{ID: id, ListFields: f, Position: pos, Notify: true}, nil
}

// UpdateList changes all fields of list id. Moving it to another heading puts
// it at the end there.
func UpdateList(ctx context.Context, db *sql.DB, userID, id int64, f ListFields) (List, error) {
	if err := f.clean(); err != nil {
		return List{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return List{}, err
	}
	defer tx.Rollback()

	var headingID, pos int64
	var notify bool
	err = tx.QueryRowContext(ctx, "SELECT heading_id, position, notify FROM lists WHERE id = ? AND user_id = ?",
		id, userID).Scan(&headingID, &pos, &notify)
	if errors.Is(err, sql.ErrNoRows) {
		return List{}, ErrNotFound
	}
	if err != nil {
		return List{}, err
	}
	if f.HeadingID != headingID {
		if ok, err := headingExists(ctx, tx, userID, f.HeadingID); err != nil {
			return List{}, err
		} else if !ok {
			return List{}, ErrParentNotFound
		}
		if pos, err = nextPosition(ctx, tx, userID, &f.HeadingID); err != nil {
			return List{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lists SET heading_id = ?, name = ?, position = ?, lang1 = ?, lang2 = ?,
			direction = ?, strict_case = ?, strict_punct = ?, strict_accents = ?
		WHERE id = ? AND user_id = ?`,
		f.HeadingID, f.Name, pos, f.Lang1, f.Lang2, f.Direction,
		f.StrictCase, f.StrictPunct, f.StrictAccents, id, userID); err != nil {
		return List{}, err
	}
	if err := tx.Commit(); err != nil {
		return List{}, err
	}
	return List{ID: id, ListFields: f, Position: pos, Notify: notify}, nil
}

// DeleteList removes list id (later with its cards, through ON DELETE CASCADE).
func DeleteList(ctx context.Context, db *sql.DB, userID, id int64) error {
	res, err := db.ExecContext(ctx, "DELETE FROM lists WHERE id = ? AND user_id = ?", id, userID)
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

// GetList returns list id of userID.
func GetList(ctx context.Context, db *sql.DB, userID, id int64) (List, error) {
	l, err := scanList(db.QueryRowContext(ctx, "SELECT "+listColumns+" FROM lists WHERE id = ? AND user_id = ?", id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return List{}, ErrNotFound
	}
	return l, err
}
