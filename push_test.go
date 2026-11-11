package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Damba017/leitner/internal/library"
	"github.com/Damba017/leitner/internal/notify"
	"github.com/Damba017/leitner/internal/webpush"
)

// subscribeBody is what the page posts for a browser subscribing at endpoint.
func subscribeBody(t *testing.T, endpoint string) string {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	rand.Read(secret)
	b, _ := json.Marshal(map[string]any{
		"endpoint": endpoint,
		"keys": map[string]string{
			"p256dh": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
			"auth":   base64.RawURLEncoding.EncodeToString(secret),
		},
		"label": "Safari · Mac",
	})
	return string(b)
}

type notifyJSON struct {
	PublicKey string           `json:"publicKey"`
	CanSend   bool             `json:"canSend"`
	Devices   []pushDeviceJSON `json:"devices"`
	Settings  notifySettingsJSON
}

// sent is one call of a fake sendFunc.
type sent struct {
	userID int64
	msg    pushMessage
	opts   webpush.Options
}

// fakeSend replaces the app's push service: it records each message and
// answers with rep.
func (a *testApp) fakeSend(rep webpush.Report) *[]sent {
	var got []sent
	a.s.notifier.send = func(_ context.Context, userID int64, payload []byte, opts webpush.Options) (webpush.Report, error) {
		var m pushMessage
		if err := json.Unmarshal(payload, &m); err != nil {
			a.t.Errorf("payload %s: %v", payload, err)
		}
		got = append(got, sent{userID, m, opts})
		return rep, nil
	}
	return &got
}

// mustCards gives userID a heading with one list (direction 1-2) holding cards.
func (a *testApp) mustCards(userID int64, list string, fronts ...string) (listID int64, cards []int64) {
	a.t.Helper()
	ctx := context.Background()
	h, err := library.CreateHeading(ctx, a.db, userID, "Kop "+list, nil)
	if err != nil {
		a.t.Fatal(err)
	}
	l, err := library.CreateList(ctx, a.db, userID, library.ListFields{HeadingID: h.ID, Name: list,
		Lang1: "es", Lang2: "nl", Direction: library.Dir12})
	if err != nil {
		a.t.Fatal(err)
	}
	for _, f := range fronts {
		c, err := library.CreateCard(ctx, a.db, userID, l.ID, library.CardFields{Front: f, Back: "nl " + f})
		if err != nil {
			a.t.Fatal(err)
		}
		cards = append(cards, c.ID)
	}
	return l.ID, cards
}

func TestNotifyAPI(t *testing.T) {
	a := newTestApp(t, config{})
	anna, bert := a.addUser("anna", false), a.addUser("bert", false)
	ca, cb := a.session(anna), a.session(bert)

	got := decode[notifyJSON](t, a.doJSON("GET", "/api/notify", "", ca))
	if key, err := base64.RawURLEncoding.DecodeString(got.PublicKey); err != nil || len(key) != 65 {
		t.Errorf("public key %q: %d bytes, %v", got.PublicKey, len(key), err)
	}
	if got.CanSend || len(got.Devices) != 0 {
		t.Errorf("without -url: %+v", got)
	}
	if want := (notifySettingsJSON{Interval: 60, QuietOn: true, QuietFrom: "22:00", QuietUntil: "08:00"}); got.Settings != want {
		t.Errorf("default settings = %+v", got.Settings)
	}
	wantStatus(t, a.doJSON("GET", "/api/notify", "", nil), http.StatusUnauthorized, "")

	t.Run("settings", func(t *testing.T) {
		put := func(body string) *httptest.ResponseRecorder {
			return a.doJSON("PUT", "/api/notify/settings", body, ca)
		}
		wantStatus(t, put(`{"interval":180,"quietOn":false,"quietFrom":"23:30","quietUntil":"07:15"}`), http.StatusNoContent, "")
		got := decode[notifyJSON](t, a.doJSON("GET", "/api/notify", "", ca))
		if want := (notifySettingsJSON{Interval: 180, QuietOn: false, QuietFrom: "23:30", QuietUntil: "07:15"}); got.Settings != want {
			t.Errorf("after saving: %+v", got.Settings)
		}
		if bert := decode[notifyJSON](t, a.doJSON("GET", "/api/notify", "", cb)); bert.Settings.Interval != 60 {
			t.Errorf("bert's settings changed: %+v", bert.Settings)
		}
		for body, msg := range map[string]string{
			`{"interval":45,"quietOn":true,"quietFrom":"22:00","quietUntil":"08:00"}`:      "Kies een interval",
			`{"interval":60,"quietOn":true,"quietFrom":"25:00","quietUntil":"08:00"}`:      "geldige tijden",
			`{"interval":60,"quietOn":true,"quietFrom":"22:00","quietUntil":""}`:           "geldige tijden",
			`{"interval":60,"quietOn":true,"quietFrom":"08:00","quietUntil":"08:00"}`:      "andere tijd eindigen",
			`{"interval":60,"quietOn":true,"quietFrom":"22:00","quietUntil":"8:00","x":1}`: "Ongeldige aanvraag",
		} {
			wantStatus(t, put(body), http.StatusBadRequest, msg)
		}
	})

	t.Run("devices", func(t *testing.T) {
		rec := a.doJSON("POST", "/api/push/subscriptions", subscribeBody(t, "https://web.push.apple.com/mac"), ca)
		wantStatus(t, rec, http.StatusCreated, `"id"`)
		id := decode[map[string]int64](t, rec)["id"]
		got := decode[notifyJSON](t, a.doJSON("GET", "/api/notify", "", ca))
		if len(got.Devices) != 1 || got.Devices[0].ID != id || got.Devices[0].Label != "Safari · Mac" ||
			got.Devices[0].Endpoint != "https://web.push.apple.com/mac" {
			t.Errorf("devices = %+v", got.Devices)
		}
		if bert := decode[notifyJSON](t, a.doJSON("GET", "/api/notify", "", cb)); len(bert.Devices) != 0 {
			t.Errorf("bert sees %+v", bert.Devices)
		}
		wantStatus(t, a.doJSON("POST", "/api/push/subscriptions", subscribeBody(t, "http://web.push.apple.com/x"), ca),
			http.StatusBadRequest, "ongeldig pushadres")
		wantStatus(t, a.doJSON("POST", "/api/push/subscriptions", subscribeBody(t, "https://push.example.com/x"), ca),
			http.StatusBadRequest, "kent de pushdienst")
		wantStatus(t, a.doJSON("POST", "/api/push/subscriptions",
			`{"endpoint":"https://web.push.apple.com/k","keys":{"p256dh":"AAAA","auth":"AAAA"}}`, ca),
			http.StatusBadRequest, "ongeldige sleutels")

		path := fmt.Sprintf("/api/push/subscriptions/%d", id)
		wantStatus(t, a.doJSON("DELETE", path, "", cb), http.StatusNotFound, "")
		wantStatus(t, a.doJSON("DELETE", path, "", ca), http.StatusNoContent, "")
		wantStatus(t, a.doJSON("DELETE", path, "", ca), http.StatusNotFound, "")
	})

	t.Run("logging out ends the subscription", func(t *testing.T) {
		phone := a.session(anna)
		wantStatus(t, a.doJSON("POST", "/api/push/subscriptions", subscribeBody(t, "https://web.push.apple.com/phone"), phone),
			http.StatusCreated, "")
		wantStatus(t, a.doJSON("POST", "/api/push/subscriptions", subscribeBody(t, "https://web.push.apple.com/mac2"), ca),
			http.StatusCreated, "")
		wantRedirect(t, a.do("POST", "/logout", nil, phone), "/login")
		got := decode[notifyJSON](t, a.doJSON("GET", "/api/notify", "", ca))
		if len(got.Devices) != 1 || got.Devices[0].Endpoint != "https://web.push.apple.com/mac2" {
			t.Errorf("after logging out the phone: %+v", got.Devices)
		}
	})

	t.Run("switches", func(t *testing.T) {
		listID, _ := a.mustCards(anna, "Maanden", "enero")
		tree := decode[library.Tree](t, a.doJSON("GET", "/api/tree", "", ca))
		headingID := tree.Headings[0].ID
		if !tree.Headings[0].Notify || !tree.Lists[0].Notify {
			t.Errorf("new heading and list should be on: %+v", tree)
		}
		wantStatus(t, a.doJSON("PUT", fmt.Sprintf("/api/lists/%d/notify", listID), `{"on":false}`, ca), http.StatusNoContent, "")
		wantStatus(t, a.doJSON("PUT", fmt.Sprintf("/api/headings/%d/notify", headingID), `{"on":false}`, ca), http.StatusNoContent, "")
		tree = decode[library.Tree](t, a.doJSON("GET", "/api/tree", "", ca))
		if tree.Headings[0].Notify || tree.Lists[0].Notify {
			t.Errorf("switched off: %+v", tree)
		}
		wantStatus(t, a.doJSON("PUT", fmt.Sprintf("/api/lists/%d/notify", listID), `{"on":true}`, cb), http.StatusNotFound, "")
		wantStatus(t, a.doJSON("PUT", fmt.Sprintf("/api/headings/%d/notify", headingID), `{"on":true}`, cb), http.StatusNotFound, "")
		wantStatus(t, a.doJSON("PUT", fmt.Sprintf("/api/lists/%d/notify", listID), `{"on":"yes"}`, ca), http.StatusBadRequest, "")
	})

	t.Run("card", func(t *testing.T) {
		listID, cards := a.mustCards(anna, "Getallen", "uno")
		rec := a.doJSON("GET", fmt.Sprintf("/api/cards/%d?direction=2-1", cards[0]), "", ca)
		item := decode[map[string]library.PracticeItem](t, rec)["item"]
		if item.ID != cards[0] || item.ListID != listID || item.Front != "uno" || item.Direction != "2-1" || item.Box != 1 {
			t.Errorf("item = %+v", item)
		}
		wantStatus(t, a.doJSON("GET", fmt.Sprintf("/api/cards/%d?direction=both", cards[0]), "", ca), http.StatusBadRequest, "oefenrichting")
		wantStatus(t, a.doJSON("GET", fmt.Sprintf("/api/cards/%d?direction=1-2", cards[0]), "", cb), http.StatusNotFound, "")
		wantStatus(t, a.doJSON("GET", "/api/cards/x?direction=1-2", "", ca), http.StatusNotFound, "")
	})

	t.Run("test notification", func(t *testing.T) {
		wantStatus(t, a.doJSON("POST", "/api/push/test", "", cb), http.StatusConflict, "-url")
		sends := a.fakeSend(webpush.Report{Sent: 1})
		rec := a.doJSON("POST", "/api/push/test", "", cb)
		wantStatus(t, rec, http.StatusOK, `"sent":1`)
		if len(*sends) != 1 || (*sends)[0].userID != bert || !strings.Contains((*sends)[0].msg.Body, "testmelding") ||
			(*sends)[0].msg.URL != "/" || (*sends)[0].opts.Urgency != "high" {
			t.Errorf("without cards: %+v", *sends)
		}
		_, cards := a.mustCards(bert, "Woorden", "un")
		a.doJSON("POST", "/api/push/test", "", cb)
		want := pushMessage{Title: "Woorden", Body: "un", URL: fmt.Sprintf("/#/kaart/%d/1-2", cards[0]), Tag: notifyTag}
		if len(*sends) != 2 || (*sends)[1].msg != want {
			t.Errorf("with a card: %+v, want %+v", (*sends)[1:], want)
		}
	})
}

func TestNotifierTick(t *testing.T) {
	a := newTestApp(t, config{baseURL: "https://leitner.example.com"})
	ctx := context.Background()
	anna, bert := a.addUser("anna", false), a.addUser("bert", false)
	ca := a.session(anna)
	wantStatus(t, a.doJSON("POST", "/api/push/subscriptions", subscribeBody(t, "https://web.push.apple.com/a"), ca), http.StatusCreated, "")
	_, cards := a.mustCards(anna, "Maanden", "enero", "febrero")
	a.mustCards(bert, "Woorden", "un") // bert has no device
	n := a.s.notifier
	n.pick = func(int) int { return 0 }
	sends := a.fakeSend(webpush.Report{Sent: 1})

	at := func(d, h, m int) time.Time { return time.Date(2026, time.November, d, h, m, 0, 0, amsterdam) }
	if err := notify.Touch(ctx, a.db, anna, at(10, 8, 30)); err != nil {
		t.Fatal(err)
	}
	step := func(now time.Time, wantSends int, wantNext time.Time) {
		t.Helper()
		next := n.tick(ctx, now)
		if len(*sends) != wantSends {
			t.Errorf("%s: %d sends, want %d", now.Format("02 15:04"), len(*sends), wantSends)
		}
		if !next.Equal(wantNext) {
			t.Errorf("%s: next look at %s, want %s", now.Format("02 15:04"), next.In(amsterdam).Format("02 15:04"), wantNext.Format("02 15:04"))
		}
	}

	step(at(10, 8, 59), 0, at(10, 9, 0))
	step(at(10, 9, 0), 1, at(10, 10, 0))
	if m := (*sends)[0]; m.userID != anna || m.msg.Body != "enero" || m.msg.Title != "Maanden" || m.opts.TTL != time.Hour {
		t.Errorf("first notification = %+v", m)
	}
	step(at(10, 9, 1), 1, at(10, 10, 0))
	// The next one is another card: enero was sent last.
	step(at(10, 10, 0), 2, at(10, 11, 0))
	if m := (*sends)[1]; m.msg.URL != fmt.Sprintf("/#/kaart/%d/1-2", cards[1]) {
		t.Errorf("second notification = %+v", m)
	}
	// Asleep since 10:00, awake in the quiet hours: nothing until 08:00.
	step(at(10, 22, 30), 2, at(11, 8, 0))
	step(at(11, 7, 58), 2, at(11, 8, 0))
	step(at(11, 8, 0), 3, at(11, 9, 0))

	// Every device failed: tried again 5 minutes later, not before.
	*sends = nil
	a.s.notifier.send = func(_ context.Context, _ int64, _ []byte, _ webpush.Options) (webpush.Report, error) {
		*sends = append(*sends, sent{})
		return webpush.Report{Failed: 1}, nil
	}
	step(at(11, 9, 0), 1, at(11, 9, 5))
	step(at(11, 9, 2), 1, at(11, 9, 5))
	step(at(11, 9, 5), 2, at(11, 9, 10))
	sends = a.fakeSend(webpush.Report{Sent: 1})
	step(at(11, 9, 10), 1, at(11, 10, 0))

	// Settings saved: counting starts from then.
	if err := notify.Save(ctx, a.db, anna, notify.Settings{Interval: 0, QuietOn: true, QuietFrom: 1320, QuietUntil: 480}, at(11, 9, 30)); err != nil {
		t.Fatal(err)
	}
	step(at(11, 10, 0), 1, at(11, 10, 5)) // off: nothing, look again in 5 minutes
	if err := notify.Save(ctx, a.db, anna, notify.Defaults, at(11, 10, 30)); err != nil {
		t.Fatal(err)
	}
	step(at(11, 10, 31), 1, at(11, 11, 0))

	// No cards in lists that are on: the time is skipped without a push.
	for _, c := range cards {
		if err := library.DeleteCard(ctx, a.db, anna, c); err != nil {
			t.Fatal(err)
		}
	}
	step(at(11, 11, 0), 1, at(11, 12, 0))
	step(at(11, 11, 5), 1, at(11, 12, 0))
}
