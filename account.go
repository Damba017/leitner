package main

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Damba017/leitner/internal/auth"
)

func (s *server) accountPage(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	s.render(w, http.StatusOK, "account.html", pageData{Me: me, Nav: "account"})
}

// changePassword needs the current password, so a stolen session (or an
// unlocked device) isn't enough to take the account over. This device stays
// logged in; the others are logged out.
func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me, _ := currentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	data := pageData{Me: me, Nav: "account"}

	// The same limiter as the login, so this form can't be used to guess either.
	userKey := "user:" + strconv.FormatInt(me.ID, 10)
	ipKey := "ip:" + clientIP(r)
	if retry, blocked := s.limiter.Blocked(ipKey, userKey); blocked {
		data.Error = tooManyAttempts(w, retry)
		s.render(w, http.StatusTooManyRequests, "account.html", data)
		return
	}
	hash, err := auth.PasswordHash(ctx, s.db, me.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	ok, err := auth.VerifyPassword(ctx, hash, r.PostFormValue("current"))
	if err != nil {
		s.serverError(w, err)
		return
	}
	if !ok {
		s.limiter.Fail(ipKey, userKey)
		slog.Info("password change: wrong current password", "user", me.Username, "ip", clientIP(r))
		data.Error = "Het huidige wachtwoord klopt niet."
		s.render(w, http.StatusUnauthorized, "account.html", data)
		return
	}
	pw := r.PostFormValue("password")
	if pw != r.PostFormValue("password2") {
		data.Error = "De nieuwe wachtwoorden zijn niet gelijk."
		s.render(w, http.StatusBadRequest, "account.html", data)
		return
	}
	var keep string
	if c, err := r.Cookie(sessionCookie); err == nil {
		keep = c.Value
	}
	err = auth.SetPassword(ctx, s.db, me.ID, pw, keep)
	if msg, known := passwordErrors[err]; known {
		data.Error = msg
		s.render(w, http.StatusBadRequest, "account.html", data)
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	slog.Info("password changed", "user", me.Username)
	data.Notice = "Wachtwoord gewijzigd. Je andere apparaten zijn uitgelogd."
	s.render(w, http.StatusOK, "account.html", data)
}
