package library

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

// setSchedule stores the state of one card direction directly.
func setSchedule(t *testing.T, db *sql.DB, card int64, dir string, box int, due time.Time) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO schedule (card_id, direction, box, due_at, reviewed_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (card_id, direction) DO UPDATE SET box = excluded.box, due_at = excluded.due_at`,
		card, dir, box, due.Unix(), due.Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyCard(t *testing.T) {
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	days := func(n int) time.Time { return now.AddDate(0, 0, n) }
	first := func(n int) int { return 0 }

	// The tree: Spaans { Maanden (1-2), Werkwoorden { Zijn (both) } }, Frans { Woorden (2-1) }.
	type fixture struct {
		db                                   *sql.DB
		anna                                 int64
		spaans, verbs, frans                 int64
		months, zijn, woorden                int64
		enero, febrero, marzo, ser, un, deux int64
	}
	setup := func(t *testing.T) fixture {
		db := testDB(t)
		f := fixture{db: db, anna: addUser(t, db, "anna")}
		f.spaans = mustHeading(t, db, f.anna, "Spaans", nil)
		f.verbs = mustHeading(t, db, f.anna, "Werkwoorden", ptr(f.spaans))
		f.frans = mustHeading(t, db, f.anna, "Frans", nil)
		list := func(heading int64, name, dir string) int64 {
			lf := fields(heading, name)
			lf.Direction = dir
			l, err := CreateList(ctx, db, f.anna, lf)
			if err != nil {
				t.Fatal(err)
			}
			return l.ID
		}
		f.months = list(f.spaans, "Maanden", Dir12)
		f.zijn = list(f.verbs, "Zijn", DirBoth)
		f.woorden = list(f.frans, "Woorden", Dir21)
		f.enero = mustCard(t, db, f.anna, f.months, "enero", "januari")
		f.febrero = mustCard(t, db, f.anna, f.months, "febrero", "februari")
		f.marzo = mustCard(t, db, f.anna, f.months, "marzo", "maart")
		f.ser = mustCard(t, db, f.anna, f.zijn, "ser", "zijn")
		f.un = mustCard(t, db, f.anna, f.woorden, "un", "een")
		f.deux = mustCard(t, db, f.anna, f.woorden, "deux", "twee")
		// Bert's card is the most overdue of all, but never Anna's.
		bert := addUser(t, db, "bert")
		bl := mustList(t, db, bert, mustHeading(t, db, bert, "Duits", nil), "Woorden")
		setSchedule(t, db, mustCard(t, db, bert, bl, "eins", "een"), Dir12, 1, days(-100))
		return f
	}
	// reviewAll gives every question of Anna a state, so none is new.
	reviewAll := func(t *testing.T, f fixture, box int, due time.Time) {
		for _, c := range []int64{f.enero, f.febrero, f.marzo} {
			setSchedule(t, f.db, c, Dir12, box, due)
		}
		setSchedule(t, f.db, f.ser, Dir12, box, due)
		setSchedule(t, f.db, f.ser, Dir21, box, due)
		setSchedule(t, f.db, f.un, Dir21, box, due)
		setSchedule(t, f.db, f.deux, Dir21, box, due)
	}
	pickOne := func(t *testing.T, f fixture, prev CardDir, pick func(int) int) (NotifyItem, bool) {
		t.Helper()
		it, ok, err := NotifyCard(ctx, f.db, f.anna, now, prev, pick)
		if err != nil {
			t.Fatal(err)
		}
		return it, ok
	}
	want := func(t *testing.T, it NotifyItem, ok bool, card int64, dir string) {
		t.Helper()
		if !ok || it.ID != card || it.Direction != dir {
			t.Errorf("got card %d %s (ok %v), want %d %s", it.ID, it.Direction, ok, card, dir)
		}
	}

	t.Run("most overdue first", func(t *testing.T) {
		f := setup(t)
		reviewAll(t, f, 3, days(5))
		setSchedule(t, f.db, f.febrero, Dir12, 2, days(-1))
		setSchedule(t, f.db, f.un, Dir21, 4, days(-3))
		it, ok := pickOne(t, f, CardDir{}, first)
		want(t, it, ok, f.un, Dir21)
		if it.Front != "un" || it.Back != "een" || it.Box != 4 || it.ListName != "Woorden" || it.ListID != f.woorden {
			t.Errorf("item = %+v", it)
		}
		// Sent last time: the next most overdue comes instead.
		it, ok = pickOne(t, f, CardDir{f.un, Dir21}, first)
		want(t, it, ok, f.febrero, Dir12)
	})

	t.Run("due before new", func(t *testing.T) {
		f := setup(t) // everything new
		setSchedule(t, f.db, f.marzo, Dir12, 5, now)
		it, ok := pickOne(t, f, CardDir{}, first)
		want(t, it, ok, f.marzo, Dir12)
	})

	t.Run("new ones at random", func(t *testing.T) {
		f := setup(t)
		setSchedule(t, f.db, f.enero, Dir12, 2, days(1)) // not due
		var n int
		it, ok := pickOne(t, f, CardDir{}, func(k int) int { n = k; return k - 1 })
		// New: febrero, marzo, ser both ways, un, deux; the last in query order.
		if n != 6 {
			t.Errorf("picked among %d new questions, want 6", n)
		}
		want(t, it, ok, f.deux, Dir21)
	})

	t.Run("ties on the due day at random", func(t *testing.T) {
		f := setup(t)
		reviewAll(t, f, 2, days(2))
		setSchedule(t, f.db, f.ser, Dir21, 1, days(-2))
		setSchedule(t, f.db, f.enero, Dir12, 3, days(-2))
		setSchedule(t, f.db, f.deux, Dir21, 1, days(-1))
		var n int
		it, ok := pickOne(t, f, CardDir{}, func(k int) int { n = k; return 1 })
		if n != 2 {
			t.Errorf("picked among %d, want 2", n)
		}
		want(t, it, ok, f.ser, Dir21) // enero, then ser in query order
		// The previous one is no candidate: the other one is all that's left.
		it, ok = pickOne(t, f, CardDir{f.enero, Dir12}, func(k int) int { n = k; return 0 })
		if n != 1 {
			t.Errorf("without the previous: picked among %d, want 1", n)
		}
		want(t, it, ok, f.ser, Dir21)
	})

	t.Run("nothing due: lowest box, soonest", func(t *testing.T) {
		f := setup(t)
		reviewAll(t, f, 4, days(3))
		setSchedule(t, f.db, f.marzo, Dir12, 2, days(5))
		setSchedule(t, f.db, f.ser, Dir12, 2, days(1))
		setSchedule(t, f.db, f.un, Dir21, 3, days(1))
		it, ok := pickOne(t, f, CardDir{}, first)
		want(t, it, ok, f.ser, Dir12)
	})

	t.Run("switched off", func(t *testing.T) {
		f := setup(t)
		reviewAll(t, f, 4, days(3))
		setSchedule(t, f.db, f.ser, Dir12, 1, days(-5))
		setSchedule(t, f.db, f.un, Dir21, 1, days(-4))
		setSchedule(t, f.db, f.enero, Dir12, 1, days(-3))

		if err := SetHeadingNotify(ctx, f.db, f.anna, f.verbs, false); err != nil {
			t.Fatal(err)
		}
		it, ok := pickOne(t, f, CardDir{}, first)
		want(t, it, ok, f.un, Dir21)

		if err := SetListNotify(ctx, f.db, f.anna, f.woorden, false); err != nil {
			t.Fatal(err)
		}
		it, ok = pickOne(t, f, CardDir{}, first)
		want(t, it, ok, f.enero, Dir12)

		// Werkwoorden back on, Spaans off: Spaans silences Werkwoorden too.
		if err := SetHeadingNotify(ctx, f.db, f.anna, f.verbs, true); err != nil {
			t.Fatal(err)
		}
		if err := SetHeadingNotify(ctx, f.db, f.anna, f.spaans, false); err != nil {
			t.Fatal(err)
		}
		if it, ok = pickOne(t, f, CardDir{}, first); ok {
			t.Errorf("everything off, got card %d", it.ID)
		}

		tree, err := GetTree(ctx, f.db, f.anna)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, h := range tree.Headings {
			got += fmt.Sprintf("%s:%v ", h.Name, h.Notify)
		}
		for _, l := range tree.Lists {
			got += fmt.Sprintf("%s:%v ", l.Name, l.Notify)
		}
		if want := "Spaans:false Frans:true Werkwoorden:true Maanden:true Zijn:true Woorden:false "; got != want {
			t.Errorf("tree flags = %q, want %q", got, want)
		}
	})

	t.Run("only the previous one", func(t *testing.T) {
		f := setup(t)
		for _, l := range []int64{f.months, f.woorden} {
			if err := SetListNotify(ctx, f.db, f.anna, l, false); err != nil {
				t.Fatal(err)
			}
		}
		setSchedule(t, f.db, f.ser, Dir12, 2, days(1))
		// ser 2-1 is new and ser 1-2 not due: new comes first, unless it was sent last.
		it, ok := pickOne(t, f, CardDir{}, first)
		want(t, it, ok, f.ser, Dir21)
		it, ok = pickOne(t, f, CardDir{f.ser, Dir21}, first)
		want(t, it, ok, f.ser, Dir12)
		if _, err := f.db.Exec("UPDATE lists SET direction = '2-1' WHERE id = ?", f.zijn); err != nil {
			t.Fatal(err)
		}
		it, ok = pickOne(t, f, CardDir{f.ser, Dir21}, first)
		want(t, it, ok, f.ser, Dir21)
	})

	t.Run("no cards", func(t *testing.T) {
		db := testDB(t)
		carol := addUser(t, db, "carol")
		mustList(t, db, carol, mustHeading(t, db, carol, "Leeg", nil), "Niets")
		if it, ok, err := NotifyCard(ctx, db, carol, now, CardDir{}, nil); err != nil || ok {
			t.Errorf("got %+v, %v, %v; want nothing", it, ok, err)
		}
	})
}

func TestSetNotifyScoped(t *testing.T) {
	db := testDB(t)
	anna, bert := addUser(t, db, "anna"), addUser(t, db, "bert")
	h := mustHeading(t, db, anna, "Spaans", nil)
	l := mustList(t, db, anna, h, "Maanden")
	if err := SetHeadingNotify(ctx, db, bert, h, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert's heading switch: %v, want ErrNotFound", err)
	}
	if err := SetListNotify(ctx, db, bert, l, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert's list switch: %v, want ErrNotFound", err)
	}
	if err := SetListNotify(ctx, db, anna, l, false); err != nil {
		t.Fatal(err)
	}
	// Switching to the same value again is fine.
	if err := SetListNotify(ctx, db, anna, l, false); err != nil {
		t.Errorf("same value again: %v", err)
	}
	// Editing the list keeps the switch.
	got, err := UpdateList(ctx, db, anna, l, fields(h, "Maanden 2"))
	if err != nil || got.Notify {
		t.Errorf("after an edit: notify = %v, %v", got.Notify, err)
	}
}

func TestPracticeCard(t *testing.T) {
	db := testDB(t)
	anna, bert := addUser(t, db, "anna"), addUser(t, db, "bert")
	l := mustList(t, db, anna, mustHeading(t, db, anna, "Spaans", nil), "Maanden")
	c := mustCard(t, db, anna, l, "enero", "januari")
	setSchedule(t, db, c, Dir21, 3, time.Now())

	it, err := PracticeCard(ctx, db, anna, c, Dir21)
	if err != nil || it.ID != c || it.ListID != l || it.Front != "enero" || it.Direction != Dir21 || it.Box != 3 {
		t.Errorf("2-1: %+v, %v", it, err)
	}
	if it, err := PracticeCard(ctx, db, anna, c, Dir12); err != nil || it.Box != 1 {
		t.Errorf("1-2 (new): %+v, %v", it, err)
	}
	if _, err := PracticeCard(ctx, db, bert, c, Dir12); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert: %v, want ErrNotFound", err)
	}
	if _, err := PracticeCard(ctx, db, anna, c, DirBoth); !errors.Is(err, ErrInvalidDirection) {
		t.Errorf("both: %v, want ErrInvalidDirection", err)
	}
}
