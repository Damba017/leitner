package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	MaxPerUser     = 20
	maxEndpointLen = 2048
	maxLabelLen    = 60
	keySetting     = "vapid_private_key"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrTooMany         = errors.New("too many subscriptions")
	ErrInvalidEndpoint = errors.New("endpoint must be an https URL")
	ErrUnknownService  = errors.New("endpoint is not on a known push service")
	ErrInvalidKeys     = errors.New("invalid subscription keys")
	ErrInvalidLabel    = errors.New("label too long")
)

// pushHosts are the push services of the browsers leitner is used in: Safari,
// Chrome (and other Chromium browsers), Firefox and Edge. The server only ever
// posts to these, so a subscription can't point it at anything else.
var pushHosts = []string{"push.apple.com", "fcm.googleapis.com", "push.services.mozilla.com", "notify.windows.com"}

// Subscription is one browser's push subscription.
type Subscription struct {
	ID       int64
	Endpoint string
	P256dh   []byte // the browser's public key, 65 bytes
	Auth     []byte // authentication secret, 16 bytes
	Label    string // e.g. "Safari · Mac", made by the page
	Created  time.Time
}

// Input is a subscription as the browser reports it (PushSubscription.toJSON):
// the keys in base64url.
type Input struct {
	Endpoint, P256dh, Auth, Label string
}

// Parse validates the input and decodes the keys.
func (in Input) Parse() (Subscription, error) {
	if err := CheckEndpoint(in.Endpoint); err != nil {
		return Subscription{}, err
	}
	p256dh, err1 := b64.DecodeString(strings.TrimRight(in.P256dh, "="))
	auth, err2 := b64.DecodeString(strings.TrimRight(in.Auth, "="))
	if err1 != nil || err2 != nil || len(auth) != authLen {
		return Subscription{}, ErrInvalidKeys
	}
	if _, err := ecdh.P256().NewPublicKey(p256dh); err != nil {
		return Subscription{}, ErrInvalidKeys
	}
	label := strings.TrimSpace(in.Label)
	if utf8.RuneCountInString(label) > maxLabelLen {
		return Subscription{}, ErrInvalidLabel
	}
	return Subscription{Endpoint: in.Endpoint, P256dh: p256dh, Auth: auth, Label: label}, nil
}

// CheckEndpoint accepts only https URLs on a known push service.
func CheckEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > maxEndpointLen || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return ErrInvalidEndpoint
	}
	if u.Port() != "" {
		return ErrUnknownService
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range pushHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return nil
		}
	}
	return ErrUnknownService
}

// Host is the push service of an endpoint, safe to log: the full endpoint
// works like a password.
func Host(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return u.Host
}

// LoadOrCreateKey returns the VAPID key from the settings table, creating it
// the first time. created reports whether that just happened.
func LoadOrCreateKey(ctx context.Context, db *sql.DB) (key *ecdsa.PrivateKey, created bool, err error) {
	var v string
	err = db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keySetting).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		if key, err = GenerateKey(); err != nil {
			return nil, false, err
		}
		raw, err := key.Bytes()
		if err != nil {
			return nil, false, err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)`, keySetting, b64.EncodeToString(raw))
		if err != nil {
			return nil, false, err
		}
		return key, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := b64.DecodeString(v)
	if err != nil {
		return nil, false, err
	}
	key, err = ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	return key, false, err
}

// Save stores sub for userID and returns its ID. An endpoint belongs to one
// browser, so when another user turns notifications on there, it moves to them.
// The subscription belongs to the login session with sessionHash
// (auth.TokenHash): when that session ends, the database drops it.
func Save(ctx context.Context, db *sql.DB, userID int64, sessionHash []byte, sub Subscription, now time.Time) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ? AND endpoint <> ?`, userID, sub.Endpoint).Scan(&n)
	if err != nil {
		return 0, err
	}
	if n >= MaxPerUser {
		return 0, ErrTooMany
	}
	// The SET expressions all see the old row, so created_at compares the
	// old user_id with the new one.
	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO push_subscriptions (user_id, session_hash, endpoint, p256dh, auth, label, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET
			created_at = CASE WHEN user_id = excluded.user_id THEN created_at ELSE excluded.created_at END,
			user_id = excluded.user_id, session_hash = excluded.session_hash,
			p256dh = excluded.p256dh, auth = excluded.auth, label = excluded.label
		RETURNING id`,
		userID, sessionHash, sub.Endpoint, sub.P256dh, sub.Auth, sub.Label, now.Unix()).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// List returns the user's subscriptions, oldest first.
func List(ctx context.Context, db *sql.DB, userID int64) ([]Subscription, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, endpoint, p256dh, auth, label, created_at FROM push_subscriptions
		WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []Subscription
	for rows.Next() {
		var s Subscription
		var created int64
		if err := rows.Scan(&s.ID, &s.Endpoint, &s.P256dh, &s.Auth, &s.Label, &created); err != nil {
			return nil, err
		}
		s.Created = time.Unix(created, 0)
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// Delete removes one of the user's subscriptions.
func Delete(ctx context.Context, db *sql.DB, userID, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Report counts what happened to each of a user's subscriptions.
type Report struct {
	Sent    int
	Removed int // expired or revoked, now deleted
	Failed  int
}

// SendToUser sends payload to all the user's subscriptions at once. Expired
// ones are deleted; other failures are logged and counted.
func SendToUser(ctx context.Context, db *sql.DB, s *Sender, userID int64, payload []byte, opts Options) (Report, error) {
	if !s.CanSend() {
		return Report{}, ErrNoSubject
	}
	if len(payload) > MaxPayload {
		return Report{}, ErrPayloadTooLarge
	}
	subs, err := List(ctx, db, userID)
	if err != nil {
		return Report{}, err
	}
	errs := make([]error, len(subs))
	var wg sync.WaitGroup
	for i, sub := range subs {
		wg.Go(func() { errs[i] = s.Send(ctx, sub, payload, opts) })
	}
	wg.Wait()

	var rep Report
	for i, err := range errs {
		host := Host(subs[i].Endpoint)
		switch {
		case err == nil:
			rep.Sent++
		case errors.Is(err, ErrGone):
			if err := Delete(ctx, db, userID, subs[i].ID); err != nil && !errors.Is(err, ErrNotFound) {
				return rep, err
			}
			rep.Removed++
			slog.Info("push subscription gone, deleted", "user_id", userID, "host", host, "label", subs[i].Label)
		default:
			rep.Failed++
			slog.Warn("push failed", "user_id", userID, "host", host, "label", subs[i].Label, "err", err)
		}
	}
	return rep, nil
}
