package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Damba017/leitner/internal/auth"
	"github.com/Damba017/leitner/internal/library"
	"github.com/Damba017/leitner/internal/notify"
	"github.com/Damba017/leitner/internal/webpush"
)

// pushErrors are the Dutch messages for webpush and notify errors, with the
// status they answer.
var pushErrors = map[error]apiErr{
	webpush.ErrInvalidEndpoint: {http.StatusBadRequest, "Deze browser gaf een ongeldig pushadres."},
	webpush.ErrUnknownService:  {http.StatusBadRequest, "Leitner kent de pushdienst van deze browser (nog) niet."},
	webpush.ErrInvalidKeys:     {http.StatusBadRequest, "Deze browser gaf ongeldige sleutels voor meldingen."},
	webpush.ErrInvalidLabel:    {http.StatusBadRequest, "De apparaatnaam is te lang."},
	webpush.ErrTooMany: {http.StatusBadRequest,
		fmt.Sprintf("Je hebt al %d apparaten met meldingen. Verwijder er eerst een.", webpush.MaxPerUser)},
	webpush.ErrNoSubject: {http.StatusConflict, "Meldingen versturen kan pas als de server met -url https://… draait."},
	webpush.ErrNotFound:  {http.StatusNotFound, "Dat apparaat staat niet (meer) in de lijst. Ververs de pagina."},
	notify.ErrInterval:   {http.StatusBadRequest, "Kies een interval uit de lijst."},
	notify.ErrTime:       {http.StatusBadRequest, "Vul geldige tijden in, bijvoorbeeld 22:00 en 08:00."},
	notify.ErrNoQuiet:    {http.StatusBadRequest, "De stille uren moeten op een andere tijd eindigen dan ze beginnen."},
}

// pushFailed answers err with its Dutch message; other errors go to apiError.
func pushFailed(w http.ResponseWriter, r *http.Request, err error) {
	for target, e := range pushErrors {
		if errors.Is(err, target) {
			writeJSONError(w, e.status, e.msg)
			return
		}
	}
	apiError(w, r, err)
}

// A test notification is only useful right away.
const testPushTTL = 5 * time.Minute

func newPushSender(ctx context.Context, db *sql.DB, subject string) (*webpush.Sender, error) {
	key, created, err := webpush.LoadOrCreateKey(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("vapid key: %w", err)
	}
	if created {
		slog.Info("created VAPID key")
	}
	return webpush.NewSender(key, subject)
}

// The endpoint lets the page recognise its own browser in the list; without
// the server's VAPID key nobody can push to it.
type pushDeviceJSON struct {
	ID       int64  `json:"id"`
	Label    string `json:"label"`
	Created  int64  `json:"created"`
	Endpoint string `json:"endpoint"`
}

// notifySettingsJSON is notify.Settings as the app sends and gets it: times
// as "22:00", like <input type="time">.
type notifySettingsJSON struct {
	Interval   int    `json:"interval"`
	QuietOn    bool   `json:"quietOn"`
	QuietFrom  string `json:"quietFrom"`
	QuietUntil string `json:"quietUntil"`
}

// apiNotify answers everything the Meldingen tab shows, except the tree.
func (s *server) apiNotify(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	subs, err := webpush.List(r.Context(), s.db, me.ID)
	if err != nil {
		apiError(w, r, err)
		return
	}
	set, err := notify.Get(r.Context(), s.db, me.ID)
	if err != nil {
		apiError(w, r, err)
		return
	}
	out := struct {
		PublicKey string             `json:"publicKey"`
		CanSend   bool               `json:"canSend"`
		Devices   []pushDeviceJSON   `json:"devices"`
		Settings  notifySettingsJSON `json:"settings"`
	}{
		PublicKey: base64.RawURLEncoding.EncodeToString(s.push.PublicKey()),
		CanSend:   s.push.CanSend(),
		Devices:   make([]pushDeviceJSON, len(subs)),
		Settings: notifySettingsJSON{Interval: set.Interval, QuietOn: set.QuietOn,
			QuietFrom: notify.FormatClock(set.QuietFrom), QuietUntil: notify.FormatClock(set.QuietUntil)},
	}
	for i, sub := range subs {
		out.Devices[i] = pushDeviceJSON{ID: sub.ID, Label: sub.Label, Created: sub.Created.Unix(), Endpoint: sub.Endpoint}
	}
	writeJSON(w, http.StatusOK, out)
}

// apiNotifySettings stores the interval and quiet hours; the next
// notification comes at the first notification time from now.
func (s *server) apiNotifySettings(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	var body notifySettingsJSON
	if !readJSON(w, r, &body) {
		return
	}
	from, err1 := notify.ParseClock(body.QuietFrom)
	until, err2 := notify.ParseClock(body.QuietUntil)
	if err := errors.Join(err1, err2); err != nil {
		pushFailed(w, r, err)
		return
	}
	set := notify.Settings{Interval: body.Interval, QuietOn: body.QuietOn, QuietFrom: from, QuietUntil: until}
	if err := notify.Save(r.Context(), s.db, me.ID, set, time.Now()); err != nil {
		pushFailed(w, r, err)
		return
	}
	s.notifier.Wake()
	w.WriteHeader(http.StatusNoContent)
}

type notifySwitch struct {
	On bool `json:"on"`
}

func (s *server) apiHeadingNotify(w http.ResponseWriter, r *http.Request) {
	s.setNotify(w, r, library.SetHeadingNotify)
}

func (s *server) apiListNotify(w http.ResponseWriter, r *http.Request) {
	s.setNotify(w, r, library.SetListNotify)
}

func (s *server) setNotify(w http.ResponseWriter, r *http.Request,
	set func(context.Context, *sql.DB, int64, int64, bool) error) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body notifySwitch
	if !readJSON(w, r, &body) {
		return
	}
	if err := set(r.Context(), s.db, me.ID, id, body.On); err != nil {
		apiError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiSubscribe turns notifications on for this browser. The subscription
// belongs to this login session: logging out ends it.
func (s *server) apiSubscribe(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		Label string `json:"label"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	in := webpush.Input{Endpoint: body.Endpoint, P256dh: body.Keys.P256dh, Auth: body.Keys.Auth, Label: body.Label}
	sub, err := in.Parse()
	if errors.Is(err, webpush.ErrUnknownService) {
		// So a browser with another push service can be added to the list.
		slog.Warn("push subscription for an unknown push service", "user", me.Username, "host", webpush.Host(body.Endpoint))
	}
	if err != nil {
		pushFailed(w, r, err)
		return
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "Niet ingelogd.")
		return
	}
	now := time.Now()
	id, err := webpush.Save(r.Context(), s.db, me.ID, auth.TokenHash(c.Value), sub, now)
	if err != nil {
		pushFailed(w, r, err)
		return
	}
	// Counting starts now: no notification right away for a time that already passed.
	if err := notify.Touch(r.Context(), s.db, me.ID, now); err != nil {
		apiError(w, r, err)
		return
	}
	slog.Info("push subscription saved", "user", me.Username, "host", webpush.Host(sub.Endpoint), "label", sub.Label)
	s.notifier.Wake()
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *server) apiUnsubscribe(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := webpush.Delete(r.Context(), s.db, me.ID, id); err != nil {
		pushFailed(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiTestPush sends, right now, the card the next notification would ask
// (or a plain test text without cards) to all the user's devices. It doesn't
// count as a notification: the next one still comes on time.
func (s *server) apiTestPush(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	msg := pushMessage{Title: "Leitner", Body: "Dit is een testmelding. Er zijn nog geen kaarten voor meldingen.", URL: "/", Tag: notifyTag}
	item, ok, err := library.NotifyCard(r.Context(), s.db, me.ID, time.Now(), library.CardDir{}, nil)
	if err != nil {
		apiError(w, r, err)
		return
	}
	if ok {
		msg = cardMessage(item)
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		apiError(w, r, err)
		return
	}
	rep, err := s.notifier.send(r.Context(), me.ID, payload, webpush.Options{TTL: testPushTTL, Urgency: "high"})
	if err != nil {
		pushFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": rep.Sent, "removed": rep.Removed, "failed": rep.Failed})
}

// apiPracticeCard answers one question, ?direction=1-2 or 2-1: the card a
// notification opens.
func (s *server) apiPracticeCard(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := library.PracticeCard(r.Context(), s.db, me.ID, id, r.URL.Query().Get("direction"))
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}
