package library

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrTooManyCards = errors.New("too many cards in one import")
	ErrMissingSide  = errors.New("a card needs two sides")
	ErrBadCSV       = errors.New("not a CSV file with the header front,back,note")
)

// LineError says which line (or CSV row) of an import is wrong. Nothing is
// imported when one is.
type LineError struct {
	Line int
	Err  error
}

func (e *LineError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }
func (e *LineError) Unwrap() error { return e.Err }

// ParsePasted reads one card per line: `side1;side2;note`, the note being
// optional and free to contain more semicolons. Empty lines are skipped.
// With swap the first part becomes side 2 (for lists typed the other way round).
func ParsePasted(text string, swap bool) ([]CardFields, error) {
	var cards []CardFields
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, ";", 3)
		if len(parts) < 2 {
			return nil, &LineError{i + 1, ErrMissingSide}
		}
		f := CardFields{Front: parts[0], Back: parts[1]}
		if len(parts) == 3 {
			f.Note = parts[2]
		}
		if swap {
			f.Front, f.Back = f.Back, f.Front
		}
		if err := f.clean(); err != nil {
			return nil, &LineError{i + 1, errCardOrMissing(f)}
		}
		if len(cards) == MaxImportCards {
			return nil, ErrTooManyCards
		}
		cards = append(cards, f)
	}
	return cards, nil
}

// errCardOrMissing tells an empty side apart from text that is too long or
// has control characters, for a better message.
func errCardOrMissing(f CardFields) error {
	if strings.TrimSpace(f.Front) == "" || strings.TrimSpace(f.Back) == "" {
		return ErrMissingSide
	}
	return ErrInvalidCard
}

// ParseCSV reads cards from CSV with the header `front,back,note` (comma,
// UTF-8, the note column optional). Row 1 is the header.
func ParseCSV(r io.Reader) ([]CardFields, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil || len(header) < 2 || len(header) > 3 {
		return nil, &LineError{1, ErrBadCSV}
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff") // Excel adds a BOM
	want := []string{"front", "back", "note"}
	for i, h := range header {
		if !strings.EqualFold(strings.TrimSpace(h), want[i]) {
			return nil, &LineError{1, ErrBadCSV}
		}
	}
	var cards []CardFields
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return cards, nil
		}
		if err != nil {
			// FieldPos has no fields to report after an error; the error knows its line.
			var pe *csv.ParseError
			line := 0
			if errors.As(err, &pe) {
				line = pe.Line
			}
			return nil, &LineError{line, ErrBadCSV}
		}
		line, _ := cr.FieldPos(0)
		if len(rec) < 2 {
			return nil, &LineError{line, ErrMissingSide}
		}
		f := CardFields{Front: rec[0], Back: rec[1]}
		if len(rec) > 2 {
			f.Note = rec[2]
		}
		if err := f.clean(); err != nil {
			return nil, &LineError{line, errCardOrMissing(f)}
		}
		if len(cards) == MaxImportCards {
			return nil, ErrTooManyCards
		}
		cards = append(cards, f)
	}
}

// WriteCSV writes the cards of list listID as `front,back,note`.
func WriteCSV(ctx context.Context, db *sql.DB, userID, listID int64, w io.Writer) error {
	cards, err := ListCards(ctx, db, userID, listID)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"front", "back", "note"}); err != nil {
		return err
	}
	for _, c := range cards {
		if err := cw.Write([]string{c.Front, c.Back, c.Note}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// Export is everything a user owns, as a backup.
type Export struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exportedAt"`
	Headings   []Heading `json:"headings"`
	Lists      []List    `json:"lists"`
	Cards      []Card    `json:"cards"`
}

// ExportUser collects the headings, lists and cards of userID.
func ExportUser(ctx context.Context, db *sql.DB, userID int64) (Export, error) {
	tree, err := GetTree(ctx, db, userID)
	if err != nil {
		return Export{}, err
	}
	e := Export{Version: 1, ExportedAt: time.Now().UTC().Truncate(time.Second),
		Headings: tree.Headings, Lists: tree.Lists, Cards: []Card{}}
	rows, err := db.QueryContext(ctx, `SELECT `+cardColumns+` FROM cards WHERE user_id = ? ORDER BY list_id, id`, userID)
	if err != nil {
		return Export{}, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return Export{}, err
		}
		e.Cards = append(e.Cards, c)
	}
	return e, rows.Err()
}
