// Package library keeps each user's tree of headings and lists.
// A heading holds sub-headings and lists in one shared, user-defined order;
// every list belongs to a heading. Every query is scoped to one user.
package library

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxNameLen = 100 // characters, after trimming
	// MaxDepth bounds the tree: a top-level heading has depth 1. It keeps the
	// sidebar usable and the recursive queries small.
	MaxDepth = 10
)

var (
	ErrNotFound       = errors.New("not found")
	ErrParentNotFound = errors.New("parent heading not found")
	ErrInvalidName    = errors.New("name must be 1-100 characters")
	ErrCycle          = errors.New("a heading can't move into itself or one of its sub-headings")
	ErrTooDeep        = errors.New("headings nested too deep")
	ErrStaleOrder     = errors.New("order doesn't match the current contents")
)

// Heading is one heading; ParentID is nil at the top level.
type Heading struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parentId"`
	Name     string `json:"name"`
	Position int64  `json:"position"`
}

// Tree is all of a user's headings and lists, each sorted by parent and
// position; the client builds the tree from it.
type Tree struct {
	Headings []Heading `json:"headings"`
	Lists    []List    `json:"lists"`
}

// GetTree returns all headings and lists of userID.
func GetTree(ctx context.Context, db *sql.DB, userID int64) (Tree, error) {
	t := Tree{Headings: []Heading{}, Lists: []List{}}
	rows, err := db.QueryContext(ctx, `SELECT id, parent_id, name, position FROM headings
		WHERE user_id = ? ORDER BY parent_id, position, id`, userID)
	if err != nil {
		return Tree{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var h Heading
		if err := rows.Scan(&h.ID, &h.ParentID, &h.Name, &h.Position); err != nil {
			return Tree{}, err
		}
		t.Headings = append(t.Headings, h)
	}
	if err := rows.Err(); err != nil {
		return Tree{}, err
	}

	rows, err = db.QueryContext(ctx, `SELECT `+listColumns+` FROM lists
		WHERE user_id = ? ORDER BY heading_id, position, id`, userID)
	if err != nil {
		return Tree{}, err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return Tree{}, err
		}
		t.Lists = append(t.Lists, l)
	}
	return t, rows.Err()
}

// cleanName trims s and checks its length. Control characters (newlines,
// tabs) would break the one-line layout of the tree, so they're refused.
func cleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxNameLen {
		return "", ErrInvalidName
	}
	if strings.ContainsFunc(s, unicode.IsControl) {
		return "", ErrInvalidName
	}
	return s, nil
}

// headingExists reports whether id is a heading of userID.
func headingExists(ctx context.Context, tx *sql.Tx, userID, id int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM headings WHERE id = ? AND user_id = ?", id, userID).Scan(&n)
	return n > 0, err
}

// nextPosition is one past the last child of parentID (nil = top level).
// "IS ?" matches NULL as well as a value.
func nextPosition(ctx context.Context, tx *sql.Tx, userID int64, parentID *int64) (int64, error) {
	var max int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(p), 0) FROM (
			SELECT position AS p FROM headings WHERE user_id = ? AND parent_id IS ?
			UNION ALL
			SELECT position FROM lists WHERE user_id = ? AND heading_id IS ?)`,
		userID, parentID, userID, parentID).Scan(&max)
	return max + 1, err
}

// depth is the level of heading id: 1 at the top. The tree has no cycles
// (UpdateHeading refuses them), so the recursion ends.
func depth(ctx context.Context, tx *sql.Tx, userID, id int64) (int, error) {
	var d int
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE up(id, parent_id, n) AS (
			SELECT id, parent_id, 1 FROM headings WHERE id = ? AND user_id = ?
			UNION ALL
			SELECT h.id, h.parent_id, up.n + 1 FROM headings h JOIN up ON h.id = up.parent_id
		) SELECT COALESCE(MAX(n), 0) FROM up`, id, userID).Scan(&d)
	return d, err
}

func eqID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
