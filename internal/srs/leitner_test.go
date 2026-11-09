package srs

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func TestLeitnerReview(t *testing.T) {
	ams, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	l := Leitner{Loc: ams}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, ams) }
	// A November afternoon (CET, UTC+1), far from a DST change.
	nov := at(2026, time.November, 9, 15, 0)
	novDay := func(d int) time.Time { return at(2026, time.November, d, 0, 0) }

	tests := []struct {
		name    string
		box     int
		correct bool
		now     time.Time
		wantBox int
		wantDue time.Time
	}{
		{"new card right", 1, true, nov, 2, novDay(11)},
		{"box 2 right", 2, true, nov, 3, novDay(13)},
		{"box 3 right", 3, true, nov, 4, novDay(17)},
		{"box 4 right", 4, true, nov, 5, novDay(25)},
		{"box 5 stays at 16 days", 5, true, nov, 5, at(2026, time.November, 25, 0, 0)},
		{"box 1 wrong", 1, false, nov, 1, novDay(10)},
		{"box 3 wrong", 3, false, nov, 1, novDay(10)},
		{"box 5 wrong", 5, false, nov, 1, novDay(10)},
		{"box out of range counts as 1", 0, true, nov, 2, novDay(11)},
		{"box above 5 is capped", 9, true, nov, 5, at(2026, time.November, 25, 0, 0)},
		// The calendar day is Amsterdam's, not UTC's.
		{"late evening", 1, true, at(2026, time.November, 9, 23, 30), 2, novDay(11)},
		{"just after midnight", 1, true, at(2026, time.November, 10, 0, 30), 2, novDay(12)},
		{"one minute before midnight", 1, false, at(2026, time.November, 9, 23, 59), 1, novDay(10)},
		// DST ends 25 October 2026, starts 29 March 2026.
		{"across end of DST", 1, true, at(2026, time.October, 24, 20, 0), 2, at(2026, time.October, 26, 0, 0)},
		{"on the day DST ends", 1, false, at(2026, time.October, 25, 2, 30), 1, at(2026, time.October, 26, 0, 0)},
		{"across start of DST", 2, true, at(2026, time.March, 28, 10, 0), 3, at(2026, time.April, 1, 0, 0)},
		{"across a month and year", 4, true, at(2026, time.December, 20, 12, 0), 5, at(2027, time.January, 5, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := l.Review(State{Box: tt.box}, tt.correct, tt.now)
			if got.Box != tt.wantBox {
				t.Errorf("box = %d, want %d", got.Box, tt.wantBox)
			}
			if !got.Due.Equal(tt.wantDue) {
				t.Errorf("due = %v, want %v", got.Due.In(ams), tt.wantDue)
			}
			if got.Due.Location() != time.UTC {
				t.Errorf("due location = %v, want UTC", got.Due.Location())
			}
		})
	}
}

func TestLeitnerNew(t *testing.T) {
	s := Leitner{Loc: time.UTC}.New()
	if s.Box != 1 || !s.Due.IsZero() {
		t.Errorf("New() = %+v, want box 1, due now", s)
	}
}

// The UTC instants behind two DST cases, so the test doesn't only compare
// against itself.
func TestLeitnerDueUTC(t *testing.T) {
	ams, _ := time.LoadLocation("Europe/Amsterdam")
	l := Leitner{Loc: ams}
	got := l.Review(State{Box: 1}, true, time.Date(2026, time.October, 24, 18, 0, 0, 0, time.UTC))
	if want := time.Date(2026, time.October, 25, 23, 0, 0, 0, time.UTC); !got.Due.Equal(want) {
		t.Errorf("after end of DST: due = %v, want %v", got.Due, want)
	}
	got = l.Review(State{Box: 2}, true, time.Date(2026, time.March, 28, 9, 0, 0, 0, time.UTC))
	if want := time.Date(2026, time.March, 31, 22, 0, 0, 0, time.UTC); !got.Due.Equal(want) {
		t.Errorf("after start of DST: due = %v, want %v", got.Due, want)
	}
}
