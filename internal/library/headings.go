package library

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CreateHeading adds a heading at the end of parentID (nil = top level).
func CreateHeading(ctx context.Context, db *sql.DB, userID int64, name string, parentID *int64) (Heading, error) {
	name, err := cleanName(name)
	if err != nil {
		return Heading{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Heading{}, err
	}
	defer tx.Rollback()
	if parentID != nil {
		d, err := depth(ctx, tx, userID, *parentID)
		if err != nil {
			return Heading{}, err
		}
		if d == 0 {
			return Heading{}, ErrParentNotFound
		}
		if d+1 > MaxDepth {
			return Heading{}, ErrTooDeep
		}
	}
	pos, err := nextPosition(ctx, tx, userID, parentID)
	if err != nil {
		return Heading{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO headings (user_id, parent_id, name, position, created_at)
		VALUES (?, ?, ?, ?, ?)`, userID, parentID, name, pos, time.Now().Unix())
	if err != nil {
		return Heading{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Heading{}, err
	}
	if err := tx.Commit(); err != nil {
		return Heading{}, err
	}
	return Heading{ID: id, ParentID: parentID, Name: name, Position: pos, Notify: true}, nil
}

// UpdateHeading renames heading id and moves it to parentID. A move puts it
// at the end of its new parent; it can't go into its own subtree.
func UpdateHeading(ctx context.Context, db *sql.DB, userID, id int64, name string, parentID *int64) (Heading, error) {
	name, err := cleanName(name)
	if err != nil {
		return Heading{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Heading{}, err
	}
	defer tx.Rollback()

	h := Heading{ID: id, Name: name}
	err = tx.QueryRowContext(ctx, "SELECT parent_id, position, notify FROM headings WHERE id = ? AND user_id = ?",
		id, userID).Scan(&h.ParentID, &h.Position, &h.Notify)
	if errors.Is(err, sql.ErrNoRows) {
		return Heading{}, ErrNotFound
	}
	if err != nil {
		return Heading{}, err
	}

	if !eqID(h.ParentID, parentID) {
		if err := checkMove(ctx, tx, userID, id, parentID); err != nil {
			return Heading{}, err
		}
		if h.Position, err = nextPosition(ctx, tx, userID, parentID); err != nil {
			return Heading{}, err
		}
		h.ParentID = parentID
	}
	if _, err := tx.ExecContext(ctx, "UPDATE headings SET name = ?, parent_id = ?, position = ? WHERE id = ? AND user_id = ?",
		h.Name, h.ParentID, h.Position, id, userID); err != nil {
		return Heading{}, err
	}
	return h, tx.Commit()
}

// checkMove refuses to move heading id under parentID if that parent is
// missing, lies inside id's own subtree, or would nest the subtree too deep.
func checkMove(ctx context.Context, tx *sql.Tx, userID, id int64, parentID *int64) error {
	if parentID == nil {
		return nil // the top level always fits: the subtree is already at most MaxDepth high
	}
	parentDepth, err := depth(ctx, tx, userID, *parentID)
	if err != nil {
		return err
	}
	if parentDepth == 0 {
		return ErrParentNotFound
	}
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE down(id, n) AS (
			SELECT id, 1 FROM headings WHERE id = ? AND user_id = ?
			UNION ALL
			SELECT h.id, down.n + 1 FROM headings h JOIN down ON h.parent_id = down.id
		) SELECT id, n FROM down`, id, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	height := 0
	for rows.Next() {
		var sub int64
		var n int
		if err := rows.Scan(&sub, &n); err != nil {
			return err
		}
		if sub == *parentID {
			return ErrCycle
		}
		height = max(height, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if parentDepth+height > MaxDepth {
		return ErrTooDeep
	}
	return nil
}

// DeleteHeading removes heading id with everything in it (ON DELETE CASCADE).
func DeleteHeading(ctx context.Context, db *sql.DB, userID, id int64) error {
	res, err := db.ExecContext(ctx, "DELETE FROM headings WHERE id = ? AND user_id = ?", id, userID)
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
