package library

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/Damba017/leitner/internal/srs"
)

func mustCard(t *testing.T, db *sql.DB, user, list int64, front, back string) int64 {
	t.Helper()
	c, err := CreateCard(ctx, db, user, list, CardFields{Front: front, Back: back})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// itemKeys is "card:direction" per item, e.g. "3:1-2", in query order.
func itemKeys(items []PracticeItem) []string {
	keys := []string{}
	for _, it := range items {
		keys = append(keys, fmt.Sprintf("%d:%s", it.ID, it.Direction))
	}
	return keys
}

func TestPractice(t *testing.T) {
	db := testDB(t)
	ams, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	sched := srs.Leitner{Loc: ams}
	day := func(d, h, m int) time.Time { return time.Date(2026, time.November, d, h, m, 0, 0, ams) }
	now := day(9, 15, 0)

	anna, bert := addUser(t, db, "anna"), addUser(t, db, "bert")
	spaans := mustHeading(t, db, anna, "Spaans", nil)
	verbs := mustHeading(t, db, anna, "Werkwoorden", ptr(spaans))
	empty := mustHeading(t, db, anna, "Leeg", nil)
	f := fields(spaans, "Maanden")
	f.Direction = Dir12
	months, err := CreateList(ctx, db, anna, f)
	if err != nil {
		t.Fatal(err)
	}
	verbList := mustList(t, db, anna, verbs, "Zijn") // direction both
	a1 := mustCard(t, db, anna, months.ID, "enero", "januari")
	a2 := mustCard(t, db, anna, months.ID, "febrero", "februari")
	b1 := mustCard(t, db, anna, verbList, "ser", "zijn")
	bertList := mustList(t, db, bert, mustHeading(t, db, bert, "Frans", nil), "Woorden")
	mustCard(t, db, bert, bertList, "un", "een")

	key := func(card int64, dir string) string { return fmt.Sprintf("%d:%s", card, dir) }
	check := func(t *testing.T, s Scope, at time.Time, due bool, want ...string) {
		t.Helper()
		items, err := PracticeItems(ctx, db, anna, s, at, due)
		if err != nil {
			t.Fatal(err)
		}
		if got := itemKeys(items); !slices.Equal(got, want) {
			t.Errorf("items = %v, want %v", got, want)
		}
	}
	record := func(t *testing.T, card int64, dir string, correct bool, at time.Time) srs.State {
		t.Helper()
		s, err := RecordReview(ctx, db, sched, anna, card, dir, correct, at)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("new cards are due", func(t *testing.T) {
		tests := []struct {
			name  string
			scope Scope
			want  []string
		}{
			{"list with one direction", Scope{ListID: months.ID}, []string{key(a1, Dir12), key(a2, Dir12)}},
			{"list with both directions", Scope{ListID: verbList}, []string{key(b1, Dir12), key(b1, Dir21)}},
			{"heading includes sub-headings", Scope{HeadingID: spaans},
				[]string{key(a1, Dir12), key(a2, Dir12), key(b1, Dir12), key(b1, Dir21)}},
			{"sub-heading only", Scope{HeadingID: verbs}, []string{key(b1, Dir12), key(b1, Dir21)}},
			{"empty heading", Scope{HeadingID: empty}, []string{}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) { check(t, tt.scope, now, true, tt.want...) })
		}
		items, _ := PracticeItems(ctx, db, anna, Scope{ListID: months.ID}, now, true)
		if items[0].Box != 1 || items[0].Front != "enero" || items[0].ListID != months.ID {
			t.Errorf("item = %+v, want enero in box 1 of its list", items[0])
		}
	})

	t.Run("free mode asks every card once", func(t *testing.T) {
		check(t, Scope{HeadingID: spaans}, now, false, key(a1, Dir12), key(a2, Dir12), key(b1, DirBoth))
		items, _ := PracticeItems(ctx, db, anna, Scope{ListID: months.ID}, now, false)
		if items[0].Box != 0 {
			t.Errorf("free item box = %d, want none", items[0].Box)
		}
	})

	t.Run("a right answer waits for its box", func(t *testing.T) {
		s := record(t, a1, Dir12, true, now)
		if s.Box != 2 || !s.Due.Equal(day(11, 0, 0)) {
			t.Fatalf("state = %+v, want box 2 due 11 Nov 00:00", s)
		}
		check(t, Scope{ListID: months.ID}, now, true, key(a2, Dir12))
		check(t, Scope{ListID: months.ID}, day(10, 23, 59), true, key(a2, Dir12))
		check(t, Scope{ListID: months.ID}, day(11, 0, 0), true, key(a1, Dir12), key(a2, Dir12))
		// Free practice doesn't look at the schedule.
		check(t, Scope{ListID: months.ID}, now, false, key(a1, Dir12), key(a2, Dir12))
		items, _ := PracticeItems(ctx, db, anna, Scope{ListID: months.ID}, day(11, 0, 0), true)
		if items[0].Box != 2 {
			t.Errorf("box = %d, want 2", items[0].Box)
		}
	})

	t.Run("state builds on the stored box", func(t *testing.T) {
		if s := record(t, a1, Dir12, true, day(11, 9, 0)); s.Box != 3 || !s.Due.Equal(day(15, 0, 0)) {
			t.Errorf("state = %+v, want box 3 due 15 Nov", s)
		}
		if s := record(t, a1, Dir12, false, day(15, 9, 0)); s.Box != 1 || !s.Due.Equal(day(16, 0, 0)) {
			t.Errorf("state = %+v, want box 1 due 16 Nov", s)
		}
	})

	t.Run("each direction has its own box", func(t *testing.T) {
		record(t, b1, Dir21, false, now)
		check(t, Scope{ListID: verbList}, now, true, key(b1, Dir12))
		check(t, Scope{ListID: verbList}, day(10, 0, 0), true, key(b1, Dir12), key(b1, Dir21))
	})

	t.Run("due counts", func(t *testing.T) {
		counts, err := DueCounts(ctx, db, anna, now)
		if err != nil {
			t.Fatal(err)
		}
		// a1 is due again on 16 Nov; b1 2-1 on 10 Nov.
		want := map[int64]int{months.ID: 1, verbList: 1}
		if !maps.Equal(counts, want) {
			t.Errorf("counts = %v, want %v", counts, want)
		}
		counts, _ = DueCounts(ctx, db, anna, day(20, 0, 0))
		if want := map[int64]int{months.ID: 2, verbList: 2}; !maps.Equal(counts, want) {
			t.Errorf("later counts = %v, want %v", counts, want)
		}
	})

	t.Run("other users", func(t *testing.T) {
		for _, s := range []Scope{{ListID: months.ID}, {HeadingID: spaans}, {}, {ListID: months.ID, HeadingID: spaans}} {
			if _, err := PracticeItems(ctx, db, bert, s, now, true); !errors.Is(err, ErrNotFound) {
				t.Errorf("bert practices %+v: err = %v, want ErrNotFound", s, err)
			}
		}
		if _, err := RecordReview(ctx, db, sched, bert, a2, Dir12, true, now); !errors.Is(err, ErrNotFound) {
			t.Errorf("bert records anna's card: err = %v, want ErrNotFound", err)
		}
		counts, _ := DueCounts(ctx, db, bert, now)
		if want := map[int64]int{bertList: 2}; !maps.Equal(counts, want) {
			t.Errorf("bert's counts = %v, want %v", counts, want)
		}
		e, err := ExportUser(ctx, db, bert)
		if err != nil || len(e.Schedule) != 0 {
			t.Errorf("bert's export schedule = %v, %v; want empty", e.Schedule, err)
		}
	})

	t.Run("invalid direction", func(t *testing.T) {
		for _, dir := range []string{DirBoth, "", "1-1"} {
			if _, err := RecordReview(ctx, db, sched, anna, a2, dir, true, now); !errors.Is(err, ErrInvalidDirection) {
				t.Errorf("direction %q: err = %v, want ErrInvalidDirection", dir, err)
			}
		}
		if _, err := RecordReview(ctx, db, sched, anna, 99999, Dir12, true, now); !errors.Is(err, ErrNotFound) {
			t.Errorf("missing card: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("export and cascade", func(t *testing.T) {
		e, err := ExportUser(ctx, db, anna)
		if err != nil {
			t.Fatal(err)
		}
		want := []ScheduleEntry{
			{CardID: a1, Direction: Dir12, Box: 1, DueAt: day(16, 0, 0).UTC(), ReviewedAt: day(15, 9, 0).UTC()},
			{CardID: b1, Direction: Dir21, Box: 1, DueAt: day(10, 0, 0).UTC(), ReviewedAt: now.UTC()},
		}
		if !slices.EqualFunc(e.Schedule, want, func(a, b ScheduleEntry) bool {
			return a.CardID == b.CardID && a.Direction == b.Direction && a.Box == b.Box &&
				a.DueAt.Equal(b.DueAt) && a.ReviewedAt.Equal(b.ReviewedAt)
		}) {
			t.Errorf("schedule = %+v, want %+v", e.Schedule, want)
		}
		if err := DeleteCard(ctx, db, anna, a1); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM schedule WHERE card_id = ?", a1).Scan(&n); err != nil || n != 0 {
			t.Errorf("schedule rows of a deleted card = %d, %v; want 0", n, err)
		}
	})
}
