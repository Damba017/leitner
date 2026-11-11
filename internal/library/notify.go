package library

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"time"
)

// SetHeadingNotify switches notifications from heading id on or off. Off
// silences everything in it, whatever its sub-headings and lists say.
func SetHeadingNotify(ctx context.Context, db *sql.DB, userID, id int64, on bool) error {
	return setNotify(ctx, db, "headings", userID, id, on)
}

// SetListNotify switches notifications from list id on or off.
func SetListNotify(ctx context.Context, db *sql.DB, userID, id int64, on bool) error {
	return setNotify(ctx, db, "lists", userID, id, on)
}

func setNotify(ctx context.Context, db *sql.DB, table string, userID, id int64, on bool) error {
	res, err := db.ExecContext(ctx, "UPDATE "+table+" SET notify = ? WHERE id = ? AND user_id = ?", on, id, userID)
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

// CardDir is one direction of a card: one question. The zero value is none.
type CardDir struct {
	CardID    int64
	Direction string
}

// NotifyItem is the question of a notification, with its list's name.
type NotifyItem struct {
	PracticeItem
	ListName string
}

// NotifyCard picks the question for a notification to userID at now, from
// the lists whose notifications are on (a heading that's off silences
// everything in it). First choice: a reviewed question that is due, the one
// due longest; then a new one; when nothing is due, one from the lowest box,
// the soonest due. The questions tied for first are picked from at random
// (pick(n) returns 0..n-1; nil uses math/rand). prev, the question sent last
// time, is left out unless it's the only one. ok is false when there is none.
func NotifyCard(ctx context.Context, db *sql.DB, userID int64, now time.Time, prev CardDir,
	pick func(n int) int) (item NotifyItem, ok bool, err error) {
	if pick == nil {
		pick = rand.IntN
	}
	// grp: 0 = reviewed and due, 1 = new, 2 = not due yet. Within a group,
	// (k1, k2) ranks: due date / nothing / box, then due date.
	rows, err := db.QueryContext(ctx, `WITH RECURSIVE on_headings(id) AS (
				SELECT id FROM headings WHERE user_id = ? AND parent_id IS NULL AND notify = 1
				UNION ALL
				SELECT h.id FROM headings h JOIN on_headings o ON h.parent_id = o.id WHERE h.notify = 1
			),
			on_lists(id, name, direction) AS (
				SELECT l.id, l.name, l.direction FROM lists l JOIN on_headings o ON l.heading_id = o.id
				WHERE l.user_id = ? AND l.notify = 1
			),
			`+dirsCTE+`
		SELECT c.id, c.list_id, c.front, c.back, c.note, c.pronunciation, dirs.d, COALESCE(s.box, 1), o.name,
			CASE WHEN s.due_at IS NULL THEN 1 WHEN s.due_at <= ? THEN 0 ELSE 2 END AS grp,
			CASE WHEN s.due_at IS NULL THEN 0 WHEN s.due_at <= ? THEN s.due_at ELSE s.box END AS k1,
			CASE WHEN s.due_at IS NULL OR s.due_at <= ? THEN 0 ELSE s.due_at END AS k2
		FROM cards c
		JOIN on_lists o ON o.id = c.list_id
		JOIN dirs ON o.direction IN ('both', dirs.d)
		LEFT JOIN schedule s ON s.card_id = c.id AND s.direction = dirs.d
		WHERE c.user_id = ?
		ORDER BY grp, k1, k2, c.id, dirs.d`, userID, userID, now.Unix(), now.Unix(), now.Unix(), userID)
	if err != nil {
		return NotifyItem{}, false, err
	}
	defer rows.Close()

	type key struct{ grp, k1, k2 int64 }
	var group []NotifyItem
	var first key
	var prevItem *NotifyItem
	for rows.Next() {
		var it NotifyItem
		var k key
		if err := rows.Scan(&it.ID, &it.ListID, &it.Front, &it.Back, &it.Note, &it.Pronunciation,
			&it.Direction, &it.Box, &it.ListName, &k.grp, &k.k1, &k.k2); err != nil {
			return NotifyItem{}, false, err
		}
		if it.ID == prev.CardID && it.Direction == prev.Direction {
			prevItem = &it
			continue
		}
		if len(group) > 0 && k != first {
			break // past the first group
		}
		first = k
		group = append(group, it)
	}
	if err := rows.Err(); err != nil {
		return NotifyItem{}, false, err
	}
	switch {
	case len(group) > 0:
		return group[pick(len(group))], true, nil
	case prevItem != nil:
		return *prevItem, true, nil
	}
	return NotifyItem{}, false, nil
}

// PracticeCard is card cardID of userID asked in direction, with its box in
// that direction: the question a notification opens.
func PracticeCard(ctx context.Context, db *sql.DB, userID, cardID int64, direction string) (PracticeItem, error) {
	if direction != Dir12 && direction != Dir21 {
		return PracticeItem{}, ErrInvalidDirection
	}
	it := PracticeItem{Direction: direction}
	err := db.QueryRowContext(ctx, `SELECT c.id, c.list_id, c.front, c.back, c.note, c.pronunciation, COALESCE(s.box, 1)
		FROM cards c LEFT JOIN schedule s ON s.card_id = c.id AND s.direction = ?
		WHERE c.id = ? AND c.user_id = ?`, direction, cardID, userID).Scan(
		&it.ID, &it.ListID, &it.Front, &it.Back, &it.Note, &it.Pronunciation, &it.Box)
	if errors.Is(err, sql.ErrNoRows) {
		return PracticeItem{}, ErrNotFound
	}
	return it, err
}
