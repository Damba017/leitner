package main

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func passwordForm(current, pw, pw2 string) url.Values {
	return url.Values{"current": {current}, "password": {pw}, "password2": {pw2}}
}

func TestAccountPage(t *testing.T) {
	app := newTestApp(t, config{})
	cookie := app.session(app.addUser("anna", false))
	rec := app.do("GET", "/account", nil, cookie)
	wantStatus(t, rec, http.StatusOK, "anna@example.com")
	wantStatus(t, rec, http.StatusOK, `<a href="/account" aria-current="page">`)
	wantRedirect(t, app.do("GET", "/account", nil, nil), "/login")
}

func TestChangePassword(t *testing.T) {
	app := newTestApp(t, config{})
	id := app.addUser("anna", false)
	cookie, other := app.session(id), app.session(id)

	refused := []struct {
		name string
		form url.Values
		code int
		want string
	}{
		{"wrong current", passwordForm("fout", "nieuw-geheim", "nieuw-geheim"), http.StatusUnauthorized, "Het huidige wachtwoord klopt niet."},
		{"new ones differ", passwordForm(testPassword, "nieuw-geheim", "nieuw-geheim2"), http.StatusBadRequest, "niet gelijk"},
		{"too short", passwordForm(testPassword, "kort", "kort"), http.StatusBadRequest, "minstens 6 tekens"},
	}
	for _, tt := range refused {
		wantStatus(t, app.do("POST", "/account/password", tt.form, cookie), tt.code, tt.want)
		// Nothing changed: the other device is still in.
		if rec := app.do("GET", "/api/me", nil, other); rec.Code != http.StatusOK {
			t.Errorf("%s: other device logged out", tt.name)
		}
	}

	rec := app.do("POST", "/account/password", passwordForm(testPassword, "nieuw-geheim", "nieuw-geheim"), cookie)
	wantStatus(t, rec, http.StatusOK, "Wachtwoord gewijzigd.")
	wantStatus(t, app.do("GET", "/api/me", nil, cookie), http.StatusOK, "anna")
	wantStatus(t, app.do("GET", "/api/me", nil, other), http.StatusUnauthorized, "Niet ingelogd")

	wantStatus(t, app.do("POST", "/login", loginForm("anna", testPassword), nil), http.StatusUnauthorized, "Onjuiste")
	wantRedirect(t, app.do("POST", "/login", loginForm("anna", "nieuw-geheim"), nil), "/")
}

func TestChangePasswordRateLimit(t *testing.T) {
	app := newTestApp(t, config{})
	cookie := app.session(app.addUser("anna", false))
	for i := range 5 {
		form := passwordForm("fout"+strconv.Itoa(i), "nieuw-geheim", "nieuw-geheim")
		wantStatus(t, app.do("POST", "/account/password", form, cookie), http.StatusUnauthorized, "klopt niet")
	}
	form := passwordForm(testPassword, "nieuw-geheim", "nieuw-geheim")
	wantStatus(t, app.do("POST", "/account/password", form, cookie), http.StatusTooManyRequests, "Te veel mislukte pogingen")
	// The same counter as the login form.
	wantStatus(t, app.do("POST", "/login", loginForm("anna", testPassword), nil), http.StatusTooManyRequests, "Te veel")
}
