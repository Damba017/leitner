package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Damba017/leitner/internal/auth"
)

func userPath(id int64, action string) string {
	return "/admin/users/" + strconv.FormatInt(id, 10) + "/" + action
}

func isAdmin(t *testing.T, app *testApp, id int64) bool {
	t.Helper()
	u, err := auth.GetUser(t.Context(), app.db, id)
	if err != nil {
		t.Fatal(err)
	}
	return u.IsAdmin
}

func TestAdminOnly(t *testing.T) {
	app := newTestApp(t, config{})
	app.addUser("anna", true)
	bertID := app.addUser("bert", false)
	bert := app.session(bertID)
	tests := []struct {
		method, path string
		form         url.Values
	}{
		{"GET", "/admin", nil},
		{"POST", "/admin/registration", url.Values{"open": {"1"}}},
		{"POST", userPath(1, "admin"), url.Values{"admin": {"0"}}},
		{"POST", userPath(bertID, "admin"), url.Values{"admin": {"1"}}}, // not even yourself
		{"POST", userPath(1, "password"), url.Values{"password": {"overgenomen"}}},
		{"GET", userPath(1, "delete"), nil},
		{"POST", userPath(1, "delete"), url.Values{}},
	}
	for _, tt := range tests {
		wantStatus(t, app.do(tt.method, tt.path, tt.form, bert), http.StatusForbidden, "Verboden")
		if tt.method == "GET" {
			wantRedirect(t, app.do(tt.method, tt.path, nil, nil), "/login")
		} else {
			wantStatus(t, app.do(tt.method, tt.path, tt.form, nil), http.StatusUnauthorized, "Niet ingelogd")
		}
	}
	if isAdmin(t, app, bertID) || !isAdmin(t, app, 1) {
		t.Error("admin rights changed by a non-admin")
	}
	if open, _ := auth.RegistrationOpen(t.Context(), app.db); open {
		t.Error("registration opened by a non-admin")
	}
	wantRedirect(t, app.do("POST", "/login", loginForm("anna", testPassword), nil), "/")
}

func TestAdminPage(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", true))
	app.addUser("bert", false)
	rec := app.do("GET", "/admin", nil, anna)
	for _, want := range []string{
		"Registreren is <strong>gesloten</strong>", "Registratie openen",
		"<strong>anna</strong> <span class=\"badge\">admin</span> <span class=\"muted\">(jij)</span>",
		"bert@example.com", "Admin maken", "Admin intrekken",
		`action="` + userPath(2, "delete") + `"`, `<a href="/admin" aria-current="page">`,
	} {
		wantStatus(t, rec, http.StatusOK, want)
	}
	// No delete button for yourself.
	if strings.Contains(rec.Body.String(), `action="`+userPath(1, "delete")+`"`) {
		t.Error("delete button shown for the admin's own account")
	}
}

func TestAdminRegistration(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", true))
	for _, tt := range []struct {
		value    string
		wantOpen bool
		wantCode int
	}{
		{"1", true, http.StatusSeeOther},
		{"1", true, http.StatusSeeOther},
		{"", true, http.StatusBadRequest}, // a broken form changes nothing
		{"yes", true, http.StatusBadRequest},
		{"0", false, http.StatusSeeOther},
	} {
		rec := app.do("POST", "/admin/registration", url.Values{"open": {tt.value}}, anna)
		if rec.Code != tt.wantCode {
			t.Errorf("open=%q: status %d, want %d", tt.value, rec.Code, tt.wantCode)
		}
		if open, err := auth.RegistrationOpen(t.Context(), app.db); err != nil || open != tt.wantOpen {
			t.Errorf("open=%q: open = %v (%v), want %v", tt.value, open, err, tt.wantOpen)
		}
	}
	app.do("POST", "/admin/registration", url.Values{"open": {"1"}}, anna)
	wantStatus(t, app.do("GET", "/admin", nil, anna), http.StatusOK, "Registratie sluiten")
	wantStatus(t, app.do("GET", "/register", nil, nil), http.StatusOK, "Account aanmaken")
}

func TestAdminSetAdmin(t *testing.T) {
	app := newTestApp(t, config{})
	annaID := app.addUser("anna", true)
	bertID := app.addUser("bert", false)
	anna, bert := app.session(annaID), app.session(bertID)
	set := func(id int64, v string) *httptest.ResponseRecorder {
		return app.do("POST", userPath(id, "admin"), url.Values{"admin": {v}}, anna)
	}

	// The last admin can't go.
	wantStatus(t, set(annaID, "0"), http.StatusConflict, "Er moet minstens één admin overblijven.")
	wantStatus(t, set(bertID, "x"), http.StatusBadRequest, "Ongeldig formulier")
	wantStatus(t, set(bertID+10, "1"), http.StatusNotFound, "bestaat niet (meer)")
	wantStatus(t, app.do("POST", "/admin/users/abc/admin", url.Values{"admin": {"1"}}, anna), http.StatusNotFound, "Pagina niet gevonden")

	wantRedirect(t, set(bertID, "1"), "/admin")
	// It takes effect on bert's existing session right away.
	wantStatus(t, app.do("GET", "/admin", nil, bert), http.StatusOK, "Admin")

	// Revoking your own rights sends you to the app.
	wantRedirect(t, set(annaID, "0"), "/")
	if isAdmin(t, app, annaID) || !isAdmin(t, app, bertID) {
		t.Error("admin flags wrong after revoking own rights")
	}
	wantStatus(t, app.do("GET", "/admin", nil, anna), http.StatusForbidden, "Verboden")
}

func TestAdminResetPassword(t *testing.T) {
	app := newTestApp(t, config{})
	annaID := app.addUser("anna", true)
	bertID := app.addUser("bert", false)
	anna, anna2, bert := app.session(annaID), app.session(annaID), app.session(bertID)
	reset := func(id int64, pw string) *httptest.ResponseRecorder {
		return app.do("POST", userPath(id, "password"), url.Values{"password": {pw}}, anna)
	}

	wantStatus(t, reset(bertID, "kort"), http.StatusBadRequest, "minstens 6 tekens")
	wantStatus(t, app.do("GET", "/api/me", nil, bert), http.StatusOK, "bert") // refused: nothing changed
	wantStatus(t, reset(bertID+10, "nieuw-geheim"), http.StatusNotFound, "bestaat niet (meer)")

	wantStatus(t, reset(bertID, "nieuw-geheim"), http.StatusOK, "Wachtwoord van bert is gewijzigd; bert is overal uitgelogd.")
	wantStatus(t, app.do("GET", "/api/me", nil, bert), http.StatusUnauthorized, "Niet ingelogd")
	wantStatus(t, app.do("POST", "/login", loginForm("bert", testPassword), nil), http.StatusUnauthorized, "Onjuiste")
	wantRedirect(t, app.do("POST", "/login", loginForm("bert", "nieuw-geheim"), nil), "/")

	// Your own password: this device stays in, the others don't.
	wantStatus(t, reset(annaID, "ander-geheim"), http.StatusOK, "Je wachtwoord is gewijzigd")
	wantStatus(t, app.do("GET", "/api/me", nil, anna), http.StatusOK, "anna")
	wantStatus(t, app.do("GET", "/api/me", nil, anna2), http.StatusUnauthorized, "Niet ingelogd")
}

func TestAdminDeleteUser(t *testing.T) {
	app := newTestApp(t, config{})
	annaID := app.addUser("anna", true)
	bertID := app.addUser("bert", false)
	carlaID := app.addUser("carla", true)
	anna, bert := app.session(annaID), app.session(bertID)

	// Confirm first; the GET deletes nothing.
	rec := app.do("GET", userPath(bertID, "delete"), nil, anna)
	wantStatus(t, rec, http.StatusOK, "Account bert verwijderen?")
	wantStatus(t, rec, http.StatusOK, `action="`+userPath(bertID, "delete")+`"`)
	wantStatus(t, rec, http.StatusOK, `href="/admin">Annuleren`)
	wantStatus(t, app.do("GET", "/api/me", nil, bert), http.StatusOK, "bert")

	for _, method := range []string{"GET", "POST"} {
		wantStatus(t, app.do(method, userPath(annaID, "delete"), url.Values{}, anna), http.StatusBadRequest,
			"Je kunt je eigen account hier niet verwijderen.")
		wantStatus(t, app.do(method, userPath(bertID+10, "delete"), url.Values{}, anna), http.StatusNotFound, "bestaat niet (meer)")
	}

	wantRedirect(t, app.do("POST", userPath(bertID, "delete"), url.Values{}, anna), "/admin")
	wantStatus(t, app.do("GET", "/api/me", nil, bert), http.StatusUnauthorized, "Niet ingelogd")
	wantStatus(t, app.do("POST", "/login", loginForm("bert", testPassword), nil), http.StatusUnauthorized, "Onjuiste")
	if strings.Contains(app.do("GET", "/admin", nil, anna).Body.String(), "bert@example.com") {
		t.Error("deleted user still listed")
	}

	// Another admin may be deleted while one is left.
	wantRedirect(t, app.do("POST", userPath(carlaID, "delete"), url.Values{}, anna), "/admin")
	users, err := auth.ListUsers(t.Context(), app.db)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != annaID {
		t.Errorf("users = %+v, want only anna", users)
	}
}
