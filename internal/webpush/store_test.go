package webpush

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Damba017/leitner/internal/auth"
	"github.com/Damba017/leitner/internal/store"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newUser(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	id, err := auth.CreateUser(t.Context(), db,
		auth.NewUser{Username: name, Email: name + "@example.com", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// newSession logs userID in and returns the session's token.
func newSession(t *testing.T, db *sql.DB, userID int64) string {
	t.Helper()
	token, err := auth.NewSession(t.Context(), db, userID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// save stores sub for userID under a new session of theirs.
func save(t *testing.T, db *sql.DB, userID int64, sub Subscription, now time.Time) int64 {
	t.Helper()
	id, err := Save(t.Context(), db, userID, auth.TokenHash(newSession(t, db, userID)), sub, now)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func list(t *testing.T, db *sql.DB, userID int64) []Subscription {
	t.Helper()
	subs, err := List(t.Context(), db, userID)
	if err != nil {
		t.Fatal(err)
	}
	return subs
}

func TestLoadOrCreateKey(t *testing.T) {
	db := newDB(t)
	first, created, err := LoadOrCreateKey(t.Context(), db)
	if err != nil || !created {
		t.Fatalf("first call: created = %v, err = %v", created, err)
	}
	again, created, err := LoadOrCreateKey(t.Context(), db)
	if err != nil || created {
		t.Fatalf("second call: created = %v, err = %v", created, err)
	}
	if !first.Equal(again) {
		t.Error("the key changed between starts")
	}
}

func TestSaveListDelete(t *testing.T) {
	db := newDB(t)
	alice, bob := newUser(t, db, "alice"), newUser(t, db, "bob")
	t0 := time.Unix(1_800_000_000, 0)

	mac, _ := newSub(t, "https://web.push.apple.com/mac")
	mac.Label = "Safari · Mac"
	phone, _ := newSub(t, "https://web.push.apple.com/phone")
	macID := save(t, db, alice, mac, t0)
	phoneID := save(t, db, alice, phone, t0.Add(time.Minute))
	laptop, _ := newSub(t, "https://updates.push.services.mozilla.com/laptop")
	save(t, db, bob, laptop, t0)

	subs := list(t, db, alice)
	if len(subs) != 2 || subs[0].ID != macID || subs[1].ID != phoneID {
		t.Fatalf("alice's list = %+v", subs)
	}
	if subs[0].Label != "Safari · Mac" || !subs[0].Created.Equal(t0) || string(subs[0].Auth) != string(mac.Auth) {
		t.Errorf("mac = %+v", subs[0])
	}

	// Saving the same browser again updates it in place.
	mac.Label = "Safari · MacBook"
	if id := save(t, db, alice, mac, t0.Add(time.Hour)); id != macID {
		t.Errorf("re-save gave id %d, want %d", id, macID)
	}
	if subs := list(t, db, alice); subs[0].Label != "Safari · MacBook" || !subs[0].Created.Equal(t0) {
		t.Errorf("after re-save: %+v", subs[0])
	}

	// Bob turns notifications on in Alice's browser: it moves to him.
	moved := save(t, db, bob, mac, t0.Add(2*time.Hour))
	if subs := list(t, db, alice); len(subs) != 1 || subs[0].ID != phoneID {
		t.Errorf("alice after the move = %+v", subs)
	}
	if subs := list(t, db, bob); len(subs) != 2 || subs[1].ID != moved || !subs[1].Created.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("bob after the move = %+v", subs)
	}

	if err := Delete(t.Context(), db, bob, phoneID); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob deletes alice's: err = %v, want ErrNotFound", err)
	}
	if err := Delete(t.Context(), db, alice, phoneID); err != nil {
		t.Fatal(err)
	}
	if subs := list(t, db, alice); len(subs) != 0 {
		t.Errorf("alice after delete = %+v", subs)
	}
}

func TestSaveMaxPerUser(t *testing.T) {
	db := newDB(t)
	alice := newUser(t, db, "alice")
	var first Subscription
	for i := range MaxPerUser {
		sub, _ := newSub(t, "https://fcm.googleapis.com/fcm/send/"+strconv.Itoa(i))
		if i == 0 {
			first = sub
		}
		save(t, db, alice, sub, time.Now())
	}
	extra, _ := newSub(t, "https://fcm.googleapis.com/fcm/send/extra")
	if _, err := Save(t.Context(), db, alice, auth.TokenHash(newSession(t, db, alice)), extra, time.Now()); !errors.Is(err, ErrTooMany) {
		t.Errorf("one too many: err = %v, want ErrTooMany", err)
	}
	if _, err := Save(t.Context(), db, alice, auth.TokenHash(newSession(t, db, alice)), first, time.Now()); err != nil {
		t.Errorf("re-saving an existing one at the limit: %v", err)
	}
}

func TestSendToUser(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/ok", "/bob":
			w.WriteHeader(http.StatusCreated)
		case "/gone":
			w.WriteHeader(http.StatusGone)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	db := newDB(t)
	alice, bob := newUser(t, db, "alice"), newUser(t, db, "bob")
	for _, path := range []string{"/ok", "/gone", "/fail"} {
		sub, _ := newSub(t, srv.URL+path)
		save(t, db, alice, sub, time.Now())
	}
	sub, _ := newSub(t, srv.URL+"/bob")
	save(t, db, bob, sub, time.Now())

	s := newSender(t, "https://leitner.example.com")
	rep, err := SendToUser(t.Context(), db, s, alice, []byte("hoi"), Options{TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if rep != (Report{Sent: 1, Removed: 1, Failed: 1}) {
		t.Errorf("report = %+v", rep)
	}
	if hits.Load() != 3 {
		t.Errorf("push service got %d requests, want 3 (not bob's)", hits.Load())
	}
	subs := list(t, db, alice)
	if len(subs) != 2 || subs[0].Endpoint != srv.URL+"/ok" || subs[1].Endpoint != srv.URL+"/fail" {
		t.Errorf("alice's subscriptions after sending = %+v", subs)
	}
	if len(list(t, db, bob)) != 1 {
		t.Error("bob's subscription was touched")
	}

	if _, err := SendToUser(t.Context(), db, newSender(t, ""), alice, []byte("hoi"), Options{}); !errors.Is(err, ErrNoSubject) {
		t.Errorf("without subject: err = %v, want ErrNoSubject", err)
	}
	if rep, err := SendToUser(t.Context(), db, s, newUser(t, db, "carol"), []byte("hoi"), Options{}); err != nil || rep != (Report{}) {
		t.Errorf("no subscriptions: %+v, %v", rep, err)
	}
}

// A subscription ends with the login session that made it: logging out,
// logging out everywhere and a password change on another device.
func TestSubscriptionEndsWithSession(t *testing.T) {
	db := newDB(t)
	alice := newUser(t, db, "alice")
	saveWith := func(token, endpoint string) {
		t.Helper()
		sub, _ := newSub(t, endpoint)
		if _, err := Save(t.Context(), db, alice, auth.TokenHash(token), sub, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	endpoints := func() []string {
		var out []string
		for _, s := range list(t, db, alice) {
			out = append(out, s.Endpoint)
		}
		return out
	}

	mac, phone, tablet := newSession(t, db, alice), newSession(t, db, alice), newSession(t, db, alice)
	saveWith(mac, "https://web.push.apple.com/mac")
	saveWith(phone, "https://web.push.apple.com/phone")
	saveWith(tablet, "https://web.push.apple.com/tablet")

	if err := auth.DeleteSession(t.Context(), db, tablet); err != nil {
		t.Fatal(err)
	}
	if got := endpoints(); len(got) != 2 || got[0] != "https://web.push.apple.com/mac" || got[1] != "https://web.push.apple.com/phone" {
		t.Fatalf("after logging out the tablet: %v", got)
	}

	// A password change on the Mac logs the phone out.
	if err := auth.SetPassword(t.Context(), db, alice, "another horse", mac); err != nil {
		t.Fatal(err)
	}
	if got := endpoints(); len(got) != 1 || got[0] != "https://web.push.apple.com/mac" {
		t.Fatalf("after a password change on the Mac: %v", got)
	}

	if err := auth.DeleteUserSessions(t.Context(), db, alice); err != nil {
		t.Fatal(err)
	}
	if got := endpoints(); len(got) != 0 {
		t.Fatalf("after logging out everywhere: %v", got)
	}

	// Logging in again on the Mac and turning notifications on reuses the endpoint.
	saveWith(newSession(t, db, alice), "https://web.push.apple.com/mac")
	if got := endpoints(); len(got) != 1 {
		t.Fatalf("after logging in again: %v", got)
	}
}
