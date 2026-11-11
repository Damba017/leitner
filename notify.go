package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/Damba017/leitner/internal/library"
	"github.com/Damba017/leitner/internal/notify"
	"github.com/Damba017/leitner/internal/webpush"
)

const (
	// When every device failed, the notification is tried again this much later.
	notifyRetry = 5 * time.Minute
	// Timers don't run while the machine sleeps, so the notifier never sleeps
	// longer than this before looking again.
	maxNotifySleep = 5 * time.Minute
	// A new notification replaces the previous one instead of piling up.
	notifyTag = "leitner-card"
	// Notifications show only a few lines; this also keeps the payload far
	// below webpush.MaxPayload.
	maxPushTitle = 100 // characters
	maxPushBody  = 200
)

// pushMessage is the payload the service worker turns into a notification.
type pushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	URL   string `json:"url,omitempty"` // own page to open on a click
	Tag   string `json:"tag,omitempty"`
}

// sendFunc pushes payload to all of a user's devices (webpush.SendToUser).
type sendFunc func(ctx context.Context, userID int64, payload []byte, opts webpush.Options) (webpush.Report, error)

// notifier sends every user with a device a card at their notification
// times. One goroutine sleeps until the next time; Wake makes it look again
// after settings or devices changed.
type notifier struct {
	db    *sql.DB
	send  sendFunc
	pick  func(n int) int // chooses among tied cards; nil = random
	wake  chan struct{}
	retry map[int64]time.Time // users whose last try failed: not again before then
}

func newNotifier(db *sql.DB, push *webpush.Sender) *notifier {
	return &notifier{
		db: db,
		send: func(ctx context.Context, userID int64, payload []byte, opts webpush.Options) (webpush.Report, error) {
			return webpush.SendToUser(ctx, db, push, userID, payload, opts)
		},
		wake:  make(chan struct{}, 1),
		retry: map[int64]time.Time{},
	}
}

// Wake makes the notifier look at the users again; it never blocks.
func (n *notifier) Wake() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

// run sends notifications until ctx ends.
func (n *notifier) run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-n.wake:
		}
		now := time.Now()
		next := n.tick(ctx, now)
		timer.Reset(min(max(next.Sub(now), time.Second), maxNotifySleep))
	}
}

// tick sends the notifications due at now and returns when the next one is
// due (run never sleeps longer than maxNotifySleep anyway).
func (n *notifier) tick(ctx context.Context, now time.Time) time.Time {
	var next time.Time
	soonest := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	users, err := notify.Subscribed(ctx, n.db)
	if err != nil {
		n.warn(ctx, err)
		return now.Add(notifyRetry)
	}
	seen := map[int64]bool{}
	for _, u := range users {
		seen[u.UserID] = true
		if !u.Due(now, amsterdam) {
			delete(n.retry, u.UserID) // e.g. settings saved since the failure
		} else if !n.retry[u.UserID].After(now) {
			if n.deliver(ctx, u, now) {
				delete(n.retry, u.UserID)
			} else {
				n.retry[u.UserID] = now.Add(notifyRetry)
			}
		}
		if at, ok := n.retry[u.UserID]; ok {
			soonest(at)
		}
		if at, ok := u.NextSlot(now, amsterdam); ok {
			soonest(at)
		}
	}
	for id := range n.retry {
		if !seen[id] {
			delete(n.retry, id) // no devices any more
		}
	}
	if next.IsZero() {
		return now.Add(maxNotifySleep)
	}
	return next
}

func (n *notifier) warn(ctx context.Context, err error) {
	if ctx.Err() == nil {
		slog.Warn("notifications", "err", err)
	}
}

// deliver sends u the card a notification asks now and records it; false
// means it should be tried again. With no card at all, the time is skipped.
func (n *notifier) deliver(ctx context.Context, u notify.User, now time.Time) bool {
	log := slog.With("user_id", u.UserID)
	prev := library.CardDir{CardID: u.LastCard, Direction: u.LastDirection}
	item, ok, err := library.NotifyCard(ctx, n.db, u.UserID, now, prev, n.pick)
	if err != nil {
		n.warn(ctx, err)
		return false
	}
	if !ok {
		if err := notify.MarkSent(ctx, n.db, u.UserID, now, 0, ""); err != nil {
			n.warn(ctx, err)
			return false
		}
		log.Info("no card for a notification, skipped")
		return true
	}
	payload, err := json.Marshal(cardMessage(item))
	if err != nil {
		n.warn(ctx, err)
		return false
	}
	// Undelivered (a phone that is off), it is dropped when the next one is due.
	ttl := min(time.Duration(u.Interval)*time.Minute, 24*time.Hour)
	rep, err := n.send(ctx, u.UserID, payload, webpush.Options{TTL: ttl, Urgency: "normal"})
	if err != nil || (rep.Sent == 0 && rep.Failed > 0) {
		if ctx.Err() == nil {
			log.Warn("notification not sent, will retry", "err", err, "failed", rep.Failed)
		}
		return false
	}
	if err := notify.MarkSent(ctx, n.db, u.UserID, now, item.ID, item.Direction); err != nil {
		n.warn(ctx, err)
		return false
	}
	log.Info("notification sent", "card_id", item.ID, "direction", item.Direction, "devices", rep.Sent)
	return true
}

// cardMessage asks item's question: the list's name as the title, the side
// asked as the text. A click opens the card in the app to answer it.
func cardMessage(item library.NotifyItem) pushMessage {
	ask := item.Front
	if item.Direction == library.Dir21 {
		ask = item.Back
	}
	return pushMessage{
		Title: shorten(item.ListName, maxPushTitle),
		Body:  shorten(ask, maxPushBody),
		URL:   fmt.Sprintf("/#/kaart/%d/%s", item.ID, item.Direction),
		Tag:   notifyTag,
	}
}

// shorten cuts s to at most n characters, ending in "…" when it was longer.
func shorten(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
