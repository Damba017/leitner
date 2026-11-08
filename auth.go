package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/Damba017/leitner/internal/auth"
)

const (
	// sessionCookie isn't nestje's name: nestje runs on the same host, and
	// browsers share cookies across ports.
	sessionCookie = "leitner_session"
	maxFormBytes  = 64 << 10
	// Longer than any valid username or email; bounds the rate limiter's keys.
	maxLoginLen = 254
)

type userKey struct{}

// currentUser returns the logged-in user, set by requireSession.
func currentUser(r *http.Request) (auth.User, bool) {
	u, ok := r.Context().Value(userKey{}).(auth.User)
	return u, ok
}

// publicRoutes work without a session; their handlers check for one themselves.
var publicRoutes = map[string]bool{"/login": true, "/register": true, "/logout": true}

// requireSession lets public paths through and needs a valid session for
// everything else, so new routes are protected by default. Logged out, a page
// request is sent to the login page; API calls and form posts get 401.
func (s *server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.publicFiles[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		u, err := s.sessionUser(w, r)
		switch {
		case err == nil:
			r = r.WithContext(context.WithValue(r.Context(), userKey{}, u))
		case !errors.Is(err, auth.ErrNoSession):
			s.serverError(w, err)
			return
		case publicRoutes[r.URL.Path]:
		case (r.Method == http.MethodGet || r.Method == http.MethodHead) && !strings.HasPrefix(r.URL.Path, "/api/"):
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		default:
			http.Error(w, "Niet ingelogd", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) sessionUser(w http.ResponseWriter, r *http.Request) (auth.User, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.User{}, auth.ErrNoSession
	}
	u, extended, err := auth.LookupSession(r.Context(), s.db, c.Value, time.Now())
	if err != nil {
		return auth.User{}, err
	}
	if extended {
		s.setSessionCookie(w, c.Value)
	}
	return u, nil
}

func (s *server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// startSession logs the user in on this device and sends them to the app.
func (s *server) startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	ctx := r.Context()
	now := time.Now()
	if err := auth.DeleteExpiredSessions(ctx, s.db, now); err != nil {
		slog.Warn("delete expired sessions", "err", err)
	}
	token, err := auth.NewSession(ctx, s.db, userID, now)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.setSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// clientIP trusts X-Forwarded-For only from the local reverse proxy
// (tailscaled, for Tailscale Funnel), and only its last entry: that's the one
// the proxy added itself. Phase 6 checks that Funnel really sends it.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			parts := strings.Split(xff[len(xff)-1], ",")
			if fwd, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				return fwd.Unmap().String()
			}
		}
	}
	return addr.String()
}

// tooManyAttempts answers a blocked password check: 429 with Retry-After,
// and the message for the page.
func tooManyAttempts(w http.ResponseWriter, retry time.Duration) string {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
	return fmt.Sprintf("Te veel mislukte pogingen. Probeer het over %d min opnieuw.", int(math.Ceil(retry.Minutes())))
}

func (s *server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := currentUser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	open, err := auth.RegistrationOpen(r.Context(), s.db)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, http.StatusOK, "login.html", pageData{RegistrationOpen: open})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	login := strings.TrimSpace(r.PostFormValue("login"))
	if len(login) > maxLoginLen {
		login = login[:maxLoginLen]
	}
	pw := r.PostFormValue("password")

	open, err := auth.RegistrationOpen(ctx, s.db)
	if err != nil {
		s.serverError(w, err)
		return
	}
	data := pageData{Login: login, RegistrationOpen: open}

	u, hash, err := auth.FindForLogin(ctx, s.db, login)
	// Keyed by user ID, so switching between username and email doesn't
	// reset the count.
	userKey := "user:" + strconv.FormatInt(u.ID, 10)
	if errors.Is(err, auth.ErrUserNotFound) {
		userKey = "name:" + strings.ToLower(login)
		err = nil
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	ip := clientIP(r)
	ipKey := "ip:" + ip

	if retry, blocked := s.limiter.Blocked(ipKey, userKey); blocked {
		slog.Info("login blocked", "ip", ip)
		data.Error = tooManyAttempts(w, retry)
		s.render(w, http.StatusTooManyRequests, "login.html", data)
		return
	}

	ok := false
	if hash != "" {
		ok, err = auth.VerifyPassword(ctx, hash, pw)
	} else {
		err = auth.VerifyDummy(ctx, pw)
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	if !ok {
		s.limiter.Fail(ipKey, userKey)
		// Not the login itself: people sometimes type their password there.
		slog.Info("login failed", "ip", ip, "known_user", hash != "")
		data.Error = "Onjuiste gebruikersnaam/e-mail of wachtwoord."
		s.render(w, http.StatusUnauthorized, "login.html", data)
		return
	}
	slog.Info("login", "user", u.Username, "ip", ip)
	s.startSession(w, r, u.ID)
}

// registrationAllowed answers 404 (as if the page doesn't exist) while
// registration is closed, and sends logged-in users to the app.
func (s *server) registrationAllowed(w http.ResponseWriter, r *http.Request) bool {
	open, err := auth.RegistrationOpen(r.Context(), s.db)
	if err != nil {
		s.serverError(w, err)
		return false
	}
	if !open {
		s.notFound(w, r)
		return false
	}
	if _, ok := currentUser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return false
	}
	return true
}

func (s *server) registerPage(w http.ResponseWriter, r *http.Request) {
	if s.registrationAllowed(w, r) {
		s.render(w, http.StatusOK, "register.html", pageData{})
	}
}

// passwordErrors are the Dutch messages for a refused new password.
var passwordErrors = map[error]string{
	auth.ErrPasswordTooShort: fmt.Sprintf("Het wachtwoord moet minstens %d tekens zijn.", auth.MinPasswordLen),
	auth.ErrPasswordTooLong:  "Het wachtwoord is te lang.",
}

var registerErrors = map[error]string{
	auth.ErrInvalidUsername:  "Gebruikersnaam: 3–32 tekens, alleen letters, cijfers, punt, _ en -.",
	auth.ErrInvalidEmail:     "Ongeldig e-mailadres.",
	auth.ErrUserExists:       "Gebruikersnaam of e-mailadres is al in gebruik.",
	auth.ErrPasswordTooShort: passwordErrors[auth.ErrPasswordTooShort],
	auth.ErrPasswordTooLong:  passwordErrors[auth.ErrPasswordTooLong],
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	if !s.registrationAllowed(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	data := pageData{
		Username: strings.TrimSpace(r.PostFormValue("username")),
		Email:    strings.TrimSpace(r.PostFormValue("email")),
	}
	pw := r.PostFormValue("password")
	if pw != r.PostFormValue("password2") {
		data.Error = "De wachtwoorden zijn niet gelijk."
		s.render(w, http.StatusBadRequest, "register.html", data)
		return
	}
	id, err := auth.CreateUser(r.Context(), s.db, auth.NewUser{Username: data.Username, Email: data.Email, Password: pw})
	if msg, ok := registerErrors[err]; ok {
		data.Error = msg
		s.render(w, http.StatusBadRequest, "register.html", data)
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	slog.Info("user registered", "user", data.Username, "ip", clientIP(r))
	s.startSession(w, r, id)
}

// logout works without a valid session, so an expired one still clears the cookie.
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := auth.DeleteSession(r.Context(), s.db, c.Value); err != nil {
			s.serverError(w, err)
			return
		}
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// logoutAll ends every session of the user, on all devices.
func (s *server) logoutAll(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	if err := auth.DeleteUserSessions(r.Context(), s.db, u.ID); err != nil {
		s.serverError(w, err)
		return
	}
	slog.Info("logged out everywhere", "user", u.Username)
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// apiMe tells the app who is logged in; it shows the Admin link only to admins.
func (s *server) apiMe(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"username": u.Username, "isAdmin": u.IsAdmin})
}
