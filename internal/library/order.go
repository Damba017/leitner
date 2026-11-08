package library

import (
	"context"
	"database/sql"
)

// Kinds of items in a heading.
const (
	KindHeading = "heading"
	KindList    = "list"
)

// Item is one child of a heading: a sub-heading or a list.
type Item struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

// Reorder sets the order of the children of parentID (nil = the top-level
// headings). items must be exactly the current children, each once; if not,
// another device changed them in between and the result would be a guess,
// so it fails with ErrStaleOrder.
func Reorder(ctx context.Context, db *sql.DB, userID int64, parentID *int64, items []Item) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if parentID != nil {
		if ok, err := headingExists(ctx, tx, userID, *parentID); err != nil {
			return err
		} else if !ok {
			return ErrNotFound
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT 'heading', id FROM headings WHERE user_id = ? AND parent_id IS ?
		UNION ALL
		SELECT 'list', id FROM lists WHERE user_id = ? AND heading_id IS ?`,
		userID, parentID, userID, parentID)
	if err != nil {
		return err
	}
	current := map[Item]bool{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.Kind, &it.ID); err != nil {
			rows.Close()
			return err
		}
		current[it] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(items) != len(current) {
		return ErrStaleOrder
	}
	seen := map[Item]bool{}
	for _, it := range items {
		if !current[it] || seen[it] {
			return ErrStaleOrder
		}
		seen[it] = true
	}

	for i, it := range items {
		table := "headings"
		if it.Kind == KindList {
			table = "lists"
		}
		// table is one of two constants, never user input.
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET position = ? WHERE id = ? AND user_id = ?",
			i+1, it.ID, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
