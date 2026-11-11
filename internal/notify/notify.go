// Package notify holds each user's notification settings and when the next
// notification is due. Notifications fall on fixed wall-clock times: from the
// end of the quiet hours, every interval, until the quiet hours start again.
package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
)

const dayMinutes = 24 * 60

// Intervals are the choices for Settings.Interval, in minutes; 0 is off.
var Intervals = []int{0, 30, 60, 120, 180, 240, dayMinutes}

// Settings are one user's, the same on all their devices. Times are minutes
// after midnight in the caller's time zone (Europe/Amsterdam).
type Settings struct {
	Interval   int  // minutes between notifications; 0 = off
	QuietOn    bool // no notifications from QuietFrom until QuietUntil
	QuietFrom  int
	QuietUntil int // also where the day's notifications start, quiet or not
}

// Defaults are the settings of a user who never changed them.
var Defaults = Settings{Interval: 60, QuietOn: true, QuietFrom: 22 * 60, QuietUntil: 8 * 60}

var (
	ErrInterval = errors.New("unknown notification interval")
	ErrTime     = errors.New("quiet hours must be a time of day")
	ErrNoQuiet  = errors.New("quiet hours must end at another time than they start")
)

// Validate checks s against the choices the app offers.
func (s Settings) Validate() error {
	if !slices.Contains(Intervals, s.Interval) {
		return ErrInterval
	}
	if s.QuietFrom < 0 || s.QuietFrom >= dayMinutes || s.QuietUntil < 0 || s.QuietUntil >= dayMinutes {
		return ErrTime
	}
	if s.QuietOn && s.QuietFrom == s.QuietUntil {
		return ErrNoQuiet
	}
	return nil
}

// FormatClock writes minutes after midnight as "08:00".
func FormatClock(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

// ParseClock reads "08:00" (what an <input type="time"> sends) as minutes
// after midnight.
func ParseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, ErrTime
	}
	return t.Hour()*60 + t.Minute(), nil
}

// active is the length of the day's notification window in minutes.
func (s Settings) active() int {
	if !s.QuietOn {
		return dayMinutes
	}
	return (s.QuietFrom - s.QuietUntil + dayMinutes) % dayMinutes
}

// slots are the notification times that start on day (a date in loc), in
// order. The window may run past midnight. Wall-clock minutes are added with
// time.Date, so 09:00 stays 09:00 on the days the clocks change.
func (s Settings) slots(y int, m time.Month, d int, loc *time.Location) []time.Time {
	if s.Interval <= 0 {
		return nil
	}
	var out []time.Time
	for k := 0; k*s.Interval < s.active(); k++ {
		out = append(out, time.Date(y, m, d, 0, s.QuietUntil+k*s.Interval, 0, 0, loc))
	}
	return out
}

// PrevSlot is the last notification time at or before t; ok is false when
// notifications are off.
func (s Settings) PrevSlot(t time.Time, loc *time.Location) (prev time.Time, ok bool) {
	y, m, d := t.In(loc).Date()
	// A slot of day d+1 is after t; one of day d-1 may run into day d.
	for _, day := range []int{d - 1, d} {
		for _, slot := range s.slots(y, m, day, loc) {
			if !slot.After(t) && (!ok || slot.After(prev)) {
				prev, ok = slot, true
			}
		}
	}
	return prev, ok
}

// NextSlot is the first notification time after t; ok is false when
// notifications are off.
func (s Settings) NextSlot(t time.Time, loc *time.Location) (next time.Time, ok bool) {
	y, m, d := t.In(loc).Date()
	for _, day := range []int{d - 1, d, d + 1} {
		for _, slot := range s.slots(y, m, day, loc) {
			if slot.After(t) && (!ok || slot.Before(next)) {
				next, ok = slot, true
			}
		}
	}
	return next, ok
}

// Quiet reports whether t falls in the quiet hours.
func (s Settings) Quiet(t time.Time, loc *time.Location) bool {
	if !s.QuietOn {
		return false
	}
	lt := t.In(loc)
	m := lt.Hour()*60 + lt.Minute()
	if s.QuietFrom < s.QuietUntil {
		return m >= s.QuietFrom && m < s.QuietUntil
	}
	return m >= s.QuietFrom || m < s.QuietUntil
}

// User is the notification state of one user with at least one device.
type User struct {
	UserID int64
	Settings
	LastSent      time.Time // when the last notification went (or its time was skipped)
	LastCard      int64     // the card of the last notification; 0 = none
	LastDirection string
}

// Due reports whether u should get a notification at now: a notification
// time has passed since the last one, and it isn't quiet now. A time missed
// while the server was down or asleep still gets one, at most one.
func (u User) Due(now time.Time, loc *time.Location) bool {
	prev, ok := u.PrevSlot(now, loc)
	return ok && prev.After(u.LastSent) && !u.Quiet(now, loc)
}

// Get returns userID's settings; the defaults if they never saved any.
func Get(ctx context.Context, db *sql.DB, userID int64) (Settings, error) {
	s := Defaults
	err := db.QueryRowContext(ctx, `SELECT interval_min, quiet_on, quiet_from, quiet_until
		FROM notify_settings WHERE user_id = ?`, userID).Scan(&s.Interval, &s.QuietOn, &s.QuietFrom, &s.QuietUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return Defaults, nil
	}
	return s, err
}

// Save stores userID's settings. The next notification comes at the first
// notification time after now, not right away for one that already passed.
func Save(ctx context.Context, db *sql.DB, userID int64, s Settings, now time.Time) error {
	if err := s.Validate(); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO notify_settings
			(user_id, interval_min, quiet_on, quiet_from, quiet_until, last_sent_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET interval_min = excluded.interval_min, quiet_on = excluded.quiet_on,
			quiet_from = excluded.quiet_from, quiet_until = excluded.quiet_until, last_sent_at = excluded.last_sent_at`,
		userID, s.Interval, s.QuietOn, s.QuietFrom, s.QuietUntil, now.Unix())
	return err
}

// Touch starts counting from now for userID (a device was just turned on),
// with the default settings if they have none yet.
func Touch(ctx context.Context, db *sql.DB, userID int64, now time.Time) error {
	d := Defaults
	_, err := db.ExecContext(ctx, `INSERT INTO notify_settings
			(user_id, interval_min, quiet_on, quiet_from, quiet_until, last_sent_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET last_sent_at = excluded.last_sent_at`,
		userID, d.Interval, d.QuietOn, d.QuietFrom, d.QuietUntil, now.Unix())
	return err
}

// Subscribed returns the state of every user with at least one device.
func Subscribed(ctx context.Context, db *sql.DB) ([]User, error) {
	d := Defaults
	rows, err := db.QueryContext(ctx, `SELECT u.id,
			COALESCE(n.interval_min, ?), COALESCE(n.quiet_on, ?), COALESCE(n.quiet_from, ?), COALESCE(n.quiet_until, ?),
			COALESCE(n.last_sent_at, 0), COALESCE(n.last_card_id, 0), COALESCE(n.last_direction, '')
		FROM users u LEFT JOIN notify_settings n ON n.user_id = u.id
		WHERE EXISTS (SELECT 1 FROM push_subscriptions p WHERE p.user_id = u.id)
		ORDER BY u.id`, d.Interval, d.QuietOn, d.QuietFrom, d.QuietUntil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var last int64
		if err := rows.Scan(&u.UserID, &u.Interval, &u.QuietOn, &u.QuietFrom, &u.QuietUntil,
			&last, &u.LastCard, &u.LastDirection); err != nil {
			return nil, err
		}
		u.LastSent = time.Unix(last, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}

// MarkSent records that userID's notification of at went out, about card in
// direction (cardID 0: there was no card to send).
func MarkSent(ctx context.Context, db *sql.DB, userID int64, at time.Time, cardID int64, direction string) error {
	var card, dir any
	if cardID != 0 {
		card, dir = cardID, direction
	}
	d := Defaults
	_, err := db.ExecContext(ctx, `INSERT INTO notify_settings
			(user_id, interval_min, quiet_on, quiet_from, quiet_until, last_sent_at, last_card_id, last_direction)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET last_sent_at = excluded.last_sent_at,
			last_card_id = excluded.last_card_id, last_direction = excluded.last_direction`,
		userID, d.Interval, d.QuietOn, d.QuietFrom, d.QuietUntil, at.Unix(), card, dir)
	return err
}
