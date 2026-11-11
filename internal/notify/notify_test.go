package notify

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/Damba017/leitner/internal/auth"
	"github.com/Damba017/leitner/internal/store"
)

var ams = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		panic(err)
	}
	return loc
}()

func at(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, ams) }

func clock(t time.Time) string { return t.In(ams).Format("02-01 15:04") }

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		s    Settings
		want error
	}{
		{Defaults, nil},
		{Settings{Interval: 0, QuietOn: true, QuietFrom: 1320, QuietUntil: 480}, nil},
		{Settings{Interval: 1440, QuietOn: false, QuietFrom: 0, QuietUntil: 0}, nil},
		{Settings{Interval: 45, QuietFrom: 1320, QuietUntil: 480}, ErrInterval},
		{Settings{Interval: -60, QuietFrom: 1320, QuietUntil: 480}, ErrInterval},
		{Settings{Interval: 60, QuietOn: true, QuietFrom: 1440, QuietUntil: 480}, ErrTime},
		{Settings{Interval: 60, QuietOn: true, QuietFrom: 600, QuietUntil: -1}, ErrTime},
		{Settings{Interval: 60, QuietOn: true, QuietFrom: 600, QuietUntil: 600}, ErrNoQuiet},
	} {
		if err := tc.s.Validate(); !errors.Is(err, tc.want) {
			t.Errorf("%+v: err = %v, want %v", tc.s, err, tc.want)
		}
	}
}

func TestClock(t *testing.T) {
	for in, want := range map[string]int{"00:00": 0, "08:00": 480, "22:30": 1350, "23:59": 1439} {
		got, err := ParseClock(in)
		if err != nil || got != want {
			t.Errorf("ParseClock(%q) = %d, %v; want %d", in, got, err, want)
		}
		if s := FormatClock(want); s != in {
			t.Errorf("FormatClock(%d) = %q, want %q", want, s, in)
		}
	}
	for _, in := range []string{"", "8", "24:00", "12:60", "08:00:00", "8:00pm"} {
		if _, err := ParseClock(in); !errors.Is(err, ErrTime) {
			t.Errorf("ParseClock(%q): err = %v, want ErrTime", in, err)
		}
	}
}

// daySlots lists the notification times between from and to, by walking NextSlot.
func daySlots(s Settings, from, to time.Time) []string {
	var out []string
	for t := from; ; {
		next, ok := s.NextSlot(t, ams)
		if !ok || next.After(to) {
			return out
		}
		out = append(out, clock(next))
		t = next
	}
}

func TestSlots(t *testing.T) {
	hourly := Defaults
	every3 := Settings{Interval: 180, QuietOn: true, QuietFrom: 22 * 60, QuietUntil: 8 * 60}
	daily := Settings{Interval: 1440, QuietOn: true, QuietFrom: 22 * 60, QuietUntil: 8*60 + 30}
	night := Settings{Interval: 120, QuietOn: true, QuietFrom: 3 * 60, QuietUntil: 20 * 60} // window over midnight
	noQuiet := Settings{Interval: 240, QuietOn: false, QuietFrom: 22 * 60, QuietUntil: 7 * 60}
	halfHour := Settings{Interval: 30, QuietOn: true, QuietFrom: 9 * 60, QuietUntil: 8 * 60}

	day := func(d int) (time.Time, time.Time) {
		return at(2026, time.November, d, 0, 0).Add(-time.Second), at(2026, time.November, d, 23, 59)
	}
	from, to := day(10)
	for _, tc := range []struct {
		name string
		s    Settings
		want []string
	}{
		{"hourly", hourly, []string{"10-11 08:00", "10-11 09:00", "10-11 10:00", "10-11 11:00", "10-11 12:00",
			"10-11 13:00", "10-11 14:00", "10-11 15:00", "10-11 16:00", "10-11 17:00", "10-11 18:00",
			"10-11 19:00", "10-11 20:00", "10-11 21:00"}},
		{"every 3 h", every3, []string{"10-11 08:00", "10-11 11:00", "10-11 14:00", "10-11 17:00", "10-11 20:00"}},
		{"daily", daily, []string{"10-11 08:30"}},
		{"over midnight", night, []string{"10-11 00:00", "10-11 02:00", "10-11 20:00", "10-11 22:00"}},
		{"no quiet hours", noQuiet, []string{"10-11 03:00", "10-11 07:00", "10-11 11:00", "10-11 15:00", "10-11 19:00", "10-11 23:00"}},
		{"half hour", halfHour, []string{"10-11 08:00", "10-11 08:30"}},
		{"off", Settings{Interval: 0, QuietOn: true, QuietFrom: 1320, QuietUntil: 480}, nil},
	} {
		if got := daySlots(tc.s, from, to); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v\nwant %v", tc.name, got, tc.want)
		}
	}

	// The clocks change at 02:00 / 03:00: wall-clock times stay the same.
	from, to = at(2026, time.March, 29, 0, 0).Add(-time.Second), at(2026, time.March, 29, 23, 59)
	if got := daySlots(every3, from, to); !slices.Equal(got, []string{"29-03 08:00", "29-03 11:00", "29-03 14:00", "29-03 17:00", "29-03 20:00"}) {
		t.Errorf("spring forward: %v", got)
	}
	from, to = at(2026, time.October, 25, 0, 0).Add(-time.Second), at(2026, time.October, 25, 23, 59)
	if got := daySlots(noQuiet, from, to); !slices.Equal(got, []string{"25-10 03:00", "25-10 07:00", "25-10 11:00", "25-10 15:00", "25-10 19:00", "25-10 23:00"}) {
		t.Errorf("fall back: %v", got)
	}
	if got := daySlots(night, from, to); !slices.Equal(got, []string{"25-10 00:00", "25-10 02:00", "25-10 20:00", "25-10 22:00"}) {
		t.Errorf("fall back, over midnight: %v", got)
	}
}

func TestPrevSlotAndDue(t *testing.T) {
	for _, tc := range []struct {
		name     string
		s        Settings
		now      time.Time
		prev     string // "" = none
		lastSent time.Time
		due      bool
	}{
		{"on the hour", Defaults, at(2026, 11, 10, 9, 0), "10-11 09:00", at(2026, 11, 10, 8, 0), true},
		{"already sent", Defaults, at(2026, 11, 10, 9, 30), "10-11 09:00", at(2026, 11, 10, 9, 0), false},
		{"sent late", Defaults, at(2026, 11, 10, 9, 30), "10-11 09:00", at(2026, 11, 10, 9, 10), false},
		{"missed ones: just one", Defaults, at(2026, 11, 10, 14, 25), "10-11 14:00", at(2026, 11, 10, 9, 0), true},
		{"quiet", Defaults, at(2026, 11, 10, 23, 0), "10-11 21:00", at(2026, 11, 10, 20, 0), false},
		{"early morning", Defaults, at(2026, 11, 11, 7, 59), "10-11 21:00", at(2026, 11, 10, 21, 0), false},
		{"first of the day", Defaults, at(2026, 11, 11, 8, 0), "11-11 08:00", at(2026, 11, 10, 21, 0), true},
		{"window over midnight", Settings{Interval: 120, QuietOn: true, QuietFrom: 180, QuietUntil: 1200},
			at(2026, 11, 11, 1, 0), "11-11 00:00", at(2026, 11, 10, 22, 0), true},
		{"quiet off", Settings{Interval: 240, QuietFrom: 1320, QuietUntil: 420},
			at(2026, 11, 11, 4, 0), "11-11 03:00", at(2026, 11, 10, 23, 0), true},
		{"off", Settings{Interval: 0, QuietOn: true, QuietFrom: 1320, QuietUntil: 480},
			at(2026, 11, 10, 9, 0), "", time.Unix(0, 0), false},
	} {
		prev, ok := tc.s.PrevSlot(tc.now, ams)
		if got := map[bool]string{true: clock(prev), false: ""}[ok]; got != tc.prev {
			t.Errorf("%s: PrevSlot = %q, want %q", tc.name, got, tc.prev)
		}
		u := User{Settings: tc.s, LastSent: tc.lastSent}
		if got := u.Due(tc.now, ams); got != tc.due {
			t.Errorf("%s: Due = %v, want %v", tc.name, got, tc.due)
		}
	}
}

func TestQuiet(t *testing.T) {
	over := Defaults                                                              // 22:00–08:00
	day := Settings{Interval: 60, QuietOn: true, QuietFrom: 600, QuietUntil: 840} // 10:00–14:00
	for _, tc := range []struct {
		s     Settings
		h, m  int
		quiet bool
	}{
		{over, 21, 59, false}, {over, 22, 0, true}, {over, 3, 0, true}, {over, 7, 59, true}, {over, 8, 0, false},
		{day, 9, 59, false}, {day, 10, 0, true}, {day, 13, 59, true}, {day, 14, 0, false},
	} {
		if got := tc.s.Quiet(at(2026, 11, 10, tc.h, tc.m), ams); got != tc.quiet {
			t.Errorf("%s %02d:%02d: quiet = %v, want %v", FormatClock(tc.s.QuietFrom), tc.h, tc.m, got, tc.quiet)
		}
	}
	off := over
	off.QuietOn = false
	if off.Quiet(at(2026, 11, 10, 3, 0), ams) {
		t.Error("quiet hours off, still quiet")
	}
}

func TestStore(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user := func(name string) int64 {
		id, err := auth.CreateUser(ctx, db, auth.NewUser{Username: name, Email: name + "@example.com", Password: "correct horse"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	device := func(userID int64, endpoint string) {
		token, err := auth.NewSession(ctx, db, userID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO push_subscriptions (user_id, session_hash, endpoint, p256dh, auth, created_at)
			VALUES (?, ?, ?, x'00', x'00', 0)`, userID, auth.TokenHash(token), endpoint); err != nil {
			t.Fatal(err)
		}
	}
	anna, bert, carol := user("anna"), user("bert"), user("carol")
	t0 := at(2026, 11, 10, 9, 30)

	if s, err := Get(ctx, db, anna); err != nil || s != Defaults {
		t.Errorf("no row: %+v, %v", s, err)
	}
	mine := Settings{Interval: 180, QuietOn: false, QuietFrom: 1380, QuietUntil: 420}
	if err := Save(ctx, db, anna, mine, t0); err != nil {
		t.Fatal(err)
	}
	if s, err := Get(ctx, db, anna); err != nil || s != mine {
		t.Errorf("after Save: %+v, %v", s, err)
	}
	if err := Save(ctx, db, anna, Settings{Interval: 45}, t0); !errors.Is(err, ErrInterval) {
		t.Errorf("invalid: err = %v", err)
	}

	if users, err := Subscribed(ctx, db); err != nil || len(users) != 0 {
		t.Errorf("no devices yet: %+v, %v", users, err)
	}
	device(anna, "https://web.push.apple.com/a1")
	device(anna, "https://web.push.apple.com/a2")
	device(bert, "https://web.push.apple.com/b")
	if err := Touch(ctx, db, bert, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := MarkSent(ctx, db, anna, t0.Add(time.Hour), 42, "2-1"); err != nil {
		t.Fatal(err)
	}
	users, err := Subscribed(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	want := []User{
		{UserID: anna, Settings: mine, LastSent: t0.Add(time.Hour), LastCard: 42, LastDirection: "2-1"},
		{UserID: bert, Settings: Defaults, LastSent: t0.Add(time.Minute)},
	}
	if len(users) != 2 {
		t.Fatalf("subscribed = %+v", users)
	}
	for i := range want {
		if u := users[i]; u.UserID != want[i].UserID || u.Settings != want[i].Settings || !u.LastSent.Equal(want[i].LastSent) ||
			u.LastCard != want[i].LastCard || u.LastDirection != want[i].LastDirection {
			t.Errorf("user %d = %+v, want %+v", i, u, want[i])
		}
	}

	// No card: the last card is cleared. Touch keeps the settings.
	if err := MarkSent(ctx, db, anna, t0.Add(2*time.Hour), 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := Touch(ctx, db, anna, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	users, _ = Subscribed(ctx, db)
	if u := users[0]; u.LastCard != 0 || u.LastDirection != "" || !u.LastSent.Equal(t0.Add(3*time.Hour)) || u.Settings != mine {
		t.Errorf("anna after Touch = %+v", u)
	}
	// A device without a settings row still counts, with the defaults.
	device(carol, "https://web.push.apple.com/c")
	users, _ = Subscribed(ctx, db)
	if len(users) != 3 || users[2].UserID != carol || users[2].Settings != Defaults || users[2].LastSent.Unix() != 0 {
		t.Errorf("carol = %+v", users)
	}
}
