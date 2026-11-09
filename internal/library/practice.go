package library

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Damba017/leitner/internal/srs"
)

// Scope is what to practice: one list, or a heading with everything below
// it. Exactly one of the two is set.
type Scope struct {
	ListID    int64
	HeadingID int64
}

// PracticeItem is one question: a card asked in one direction. In free mode
// Direction is the list's, so "both" leaves the side to the app.
type PracticeItem struct {
	Card
	Direction string `json:"direction"`
	Box       int    `json:"box,omitempty"` // due mode only
}

// scopeCTE selects (id, direction) of the lists in scope as the CTE
// "scope". Its arguments come first in a query that uses it.
func scopeCTE(userID int64, s Scope) (string, []any) {
	if s.ListID != 0 {
		return `scope(id, direction) AS (
				SELECT id, direction FROM lists WHERE id = ? AND user_id = ?
			)`, []any{s.ListID, userID}
	}
	return `down(id) AS (
				SELECT id FROM headings WHERE id = ? AND user_id = ?
				UNION ALL
				SELECT h.id FROM headings h JOIN down ON h.parent_id = down.id
			),
			scope(id, direction) AS (
				SELECT l.id, l.direction FROM lists l JOIN down ON l.heading_id = down.id WHERE l.user_id = ?
			)`, []any{s.HeadingID, userID, userID}
}

// dirsCTE is both directions, joined to a list with "direction IN ('both', d)".
const dirsCTE = `dirs(d) AS (VALUES ('1-2'), ('2-1'))`

// PracticeItems returns the questions in scope, in a fixed order (the app
// shuffles). With due set: every direction the list asks that is due at now,
// new cards included. Without: every card once, for free practice.
func PracticeItems(ctx context.Context, db *sql.DB, userID int64, s Scope, now time.Time, due bool) ([]PracticeItem, error) {
	if (s.ListID == 0) == (s.HeadingID == 0) {
		return nil, ErrNotFound
	}
	if err := scopeExists(ctx, db, userID, s); err != nil {
		return nil, err
	}
	cte, args := scopeCTE(userID, s)
	var query string
	if due {
		query = `WITH RECURSIVE ` + cte + `, ` + dirsCTE + `
			SELECT c.id, c.list_id, c.front, c.back, c.note, c.pronunciation, dirs.d, COALESCE(s.box, 1)
			FROM cards c
			JOIN scope ON scope.id = c.list_id
			JOIN dirs ON scope.direction IN ('both', dirs.d)
			LEFT JOIN schedule s ON s.card_id = c.id AND s.direction = dirs.d
			WHERE c.user_id = ? AND (s.due_at IS NULL OR s.due_at <= ?)
			ORDER BY c.list_id, c.id, dirs.d`
		args = append(args, userID, now.Unix())
	} else {
		query = `WITH RECURSIVE ` + cte + `
			SELECT c.id, c.list_id, c.front, c.back, c.note, c.pronunciation, scope.direction, 0
			FROM cards c
			JOIN scope ON scope.id = c.list_id
			WHERE c.user_id = ?
			ORDER BY c.list_id, c.id`
		args = append(args, userID)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PracticeItem{}
	for rows.Next() {
		var it PracticeItem
		if err := rows.Scan(&it.ID, &it.ListID, &it.Front, &it.Back, &it.Note, &it.Pronunciation,
			&it.Direction, &it.Box); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// scopeExists answers ErrNotFound unless the list or heading is userID's.
func scopeExists(ctx context.Context, db *sql.DB, userID int64, s Scope) error {
	table, id := "lists", s.ListID
	if s.HeadingID != 0 {
		table, id = "headings", s.HeadingID
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id = ? AND user_id = ?",
		id, userID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordReview stores an answer to card cardID asked in direction and
// returns the card's new state in that direction.
func RecordReview(ctx context.Context, db *sql.DB, sched srs.Scheduler, userID, cardID int64,
	direction string, correct bool, now time.Time) (srs.State, error) {
	if direction != Dir12 && direction != Dir21 {
		return srs.State{}, ErrInvalidDirection
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return srs.State{}, err
	}
	defer tx.Rollback()
	var box, dueAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT s.box, s.due_at FROM cards c
		LEFT JOIN schedule s ON s.card_id = c.id AND s.direction = ?
		WHERE c.id = ? AND c.user_id = ?`, direction, cardID, userID).Scan(&box, &dueAt)
	if errors.Is(err, sql.ErrNoRows) {
		return srs.State{}, ErrNotFound
	}
	if err != nil {
		return srs.State{}, err
	}
	state := sched.New()
	if box.Valid {
		state = srs.State{Box: int(box.Int64), Due: time.Unix(dueAt.Int64, 0).UTC()}
	}
	state = sched.Review(state, correct, now)
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule (card_id, direction, box, due_at, reviewed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (card_id, direction) DO UPDATE
		SET box = excluded.box, due_at = excluded.due_at, reviewed_at = excluded.reviewed_at`,
		cardID, direction, state.Box, state.Due.Unix(), now.Unix()); err != nil {
		return srs.State{}, err
	}
	if err := tx.Commit(); err != nil {
		return srs.State{}, err
	}
	return state, nil
}

// DueCounts is the number of due questions per list of userID (each
// direction counts); lists with nothing due are left out.
func DueCounts(ctx context.Context, db *sql.DB, userID int64, now time.Time) (map[int64]int, error) {
	rows, err := db.QueryContext(ctx, `WITH `+dirsCTE+`
		SELECT c.list_id, COUNT(*) FROM cards c
		JOIN lists l ON l.id = c.list_id
		JOIN dirs ON l.direction IN ('both', dirs.d)
		LEFT JOIN schedule s ON s.card_id = c.id AND s.direction = dirs.d
		WHERE c.user_id = ? AND (s.due_at IS NULL OR s.due_at <= ?)
		GROUP BY c.list_id`, userID, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[int64]int{}
	for rows.Next() {
		var list int64
		var n int
		if err := rows.Scan(&list, &n); err != nil {
			return nil, err
		}
		counts[list] = n
	}
	return counts, rows.Err()
}

// ScheduleEntry is the stored state of one card direction, for the export.
type ScheduleEntry struct {
	CardID     int64     `json:"cardId"`
	Direction  string    `json:"direction"`
	Box        int       `json:"box"`
	DueAt      time.Time `json:"dueAt"`
	ReviewedAt time.Time `json:"reviewedAt"`
}

func exportSchedule(ctx context.Context, db *sql.DB, userID int64) ([]ScheduleEntry, error) {
	rows, err := db.QueryContext(ctx, `SELECT s.card_id, s.direction, s.box, s.due_at, s.reviewed_at
		FROM schedule s JOIN cards c ON c.id = s.card_id
		WHERE c.user_id = ? ORDER BY s.card_id, s.direction`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleEntry{}
	for rows.Next() {
		var e ScheduleEntry
		var due, reviewed int64
		if err := rows.Scan(&e.CardID, &e.Direction, &e.Box, &due, &reviewed); err != nil {
			return nil, err
		}
		e.DueAt, e.ReviewedAt = time.Unix(due, 0).UTC(), time.Unix(reviewed, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
