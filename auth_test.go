package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Damba017/leitner/internal/auth"
)

func loginForm(login, pw string) url.Values {
	return url.Values{"login": {login}, "password": {pw}}
}

// responseCookie returns the session cookie rec sets, or nil.
func responseCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func TestRequireSession(t *testing.T) {
	app := newTestApp(t, config{})
	cookie := app.session(app.addUser("anna", false))
	bad := &http.Cookie{Name: sessionCookie, Value: strings.Repeat("x", 43)}

	tests := []struct {
		method, path string
		cookie       *http.Cookie
		wantStatus   int
		wantLocation string
	}{
		{"GET", "/", nil, http.StatusSeeOther, "/login"},
		{"HEAD", "/", nil, http.StatusSeeOther, "/login"},
		{"GET", "/index.html", nil, http.StatusSeeOther, "/login"},
		{"GET", "/missing", nil, http.StatusSeeOther, "/login"},
		{"GET", "/app.css/../index.html", nil, http.StatusSeeOther, "/login"},
		{"GET", "/", bad, http.StatusSeeOther, "/login"},
		{"GET", "/api/me", nil, http.StatusUnauthorized, ""},
		{"POST", "/logout-all", nil, http.StatusUnauthorized, ""},
		{"POST", "/", nil, http.StatusUnauthorized, ""},
		{"GET", "/app.css", nil, http.StatusOK, ""},
		{"GET", "/app.js", nil, http.StatusOK, ""},
		{"GET", "/login", nil, http.StatusOK, ""},
		{"GET", "/", cookie, http.StatusOK, ""},
		{"GET", "/api/me", cookie, http.StatusOK, ""},
		{"GET", "/login", cookie, http.StatusSeeOther, "/"},
	}
	for _, tt := range tests {
		rec := app.do(tt.method, tt.path, nil, tt.cookie)
		if rec.Code != tt.wantStatus || rec.Header().Get("Location") != tt.wantLocation {
			t.Errorf("%s %s (cookie %v) = %d to %q, want %d to %q", tt.method, tt.path, tt.cookie != nil,
				rec.Code, rec.Header().Get("Location"), tt.wantStatus, tt.wantLocation)
		}
	}
}

func TestLogin(t *testing.T) {
	app := newTestApp(t, config{})
	app.addUser("anna", false)

	tests := []struct {
		name, login, pw string
		wantOK          bool
	}{
		{"wrong password", "anna", "geheim2", false},
		{"unknown user", "bert", testPassword, false},
		{"unknown email", "bert@example.com", testPassword, false},
		{"empty", "", "", false},
		{"username", "anna", testPassword, true},
		{"username other case, spaces", "  ANNA ", testPassword, true},
		{"email", "Anna@Example.com", testPassword, true},
	}
	for _, tt := range tests {
		rec := app.do("POST", "/login", loginForm(tt.login, tt.pw), nil)
		c := responseCookie(rec)
		if !tt.wantOK {
			wantStatus(t, rec, http.StatusUnauthorized, "Onjuiste gebruikersnaam/e-mail of wachtwoord.")
			if c != nil {
				t.Errorf("%s: cookie set on a failed login", tt.name)
			}
			// What was typed comes back; the password doesn't.
			if tt.login != "" && !strings.Contains(rec.Body.String(), `value="`+strings.TrimSpace(tt.login)+`"`) {
				t.Errorf("%s: login not filled in again", tt.name)
			}
			if tt.pw != "" && strings.Contains(rec.Body.String(), tt.pw) {
				t.Errorf("%s: password in the page", tt.name)
			}
			continue
		}
		wantRedirect(t, rec, "/")
		if c == nil {
			t.Fatalf("%s: no session cookie", tt.name)
		}
		me := app.do("GET", "/api/me", nil, c)
		wantStatus(t, me, http.StatusOK, `"username":"anna"`)
	}
}

func TestSessionCookie(t *testing.T) {
	for _, tt := range []struct {
		cfg        config
		wantSecure bool
	}{
		{config{}, false},
		{config{baseURL: "https://leitner.example.com:8443"}, true},
	} {
		app := newTestApp(t, tt.cfg)
		app.addUser("anna", false)
		c := responseCookie(app.do("POST", "/login", loginForm("anna", testPassword), nil))
		if c == nil {
			t.Fatal("no session cookie")
		}
		if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Secure != tt.wantSecure ||
			c.MaxAge != int(auth.SessionTTL.Seconds()) || c.Domain != "" || len(c.Value) != 43 {
			t.Errorf("url %q: cookie = %+v", tt.cfg.baseURL, c)
		}
	}
}

func TestSessionRefreshCookie(t *testing.T) {
	app := newTestApp(t, config{})
	cookie := app.session(app.addUser("anna", false))
	if c := responseCookie(app.do("GET", "/api/me", nil, cookie)); c != nil {
		t.Errorf("fresh session sent a cookie again: %+v", c)
	}
	if _, err := app.db.Exec("UPDATE sessions SET last_seen = last_seen - 2*86400"); err != nil {
		t.Fatal(err)
	}
	c := responseCookie(app.do("GET", "/api/me", nil, cookie))
	if c == nil || c.Value != cookie.Value || c.MaxAge != int(auth.SessionTTL.Seconds()) {
		t.Errorf("session used after 2 days: cookie = %+v, want the same token with a new Max-Age", c)
	}
}

func TestLoginRateLimit(t *testing.T) {
	app := newTestApp(t, config{})
	app.addUser("anna", false)
	app.addUser("bert", false)
	from := func(ip string, form url.Values) *httptest.ResponseRecorder {
		r := app.request("POST", "/login", form, nil)
		r.RemoteAddr = ip + ":40000"
		return app.serve(r)
	}

	for i := range 5 {
		rec := from("192.0.2.1", loginForm("anna", "fout"+strconv.Itoa(i)))
		wantStatus(t, rec, http.StatusUnauthorized, "Onjuiste")
	}
	// Blocked now, even with the right password: per IP and per user.
	rec := from("192.0.2.1", loginForm("anna", testPassword))
	wantStatus(t, rec, http.StatusTooManyRequests, "Te veel mislukte pogingen. Probeer het over 15 min opnieuw.")
	if responseCookie(rec) != nil {
		t.Error("blocked login set a cookie")
	}
	if ra, _ := strconv.Atoi(rec.Header().Get("Retry-After")); ra < 890 || ra > 900 {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	// The user key follows the account, not what was typed.
	wantStatus(t, from("198.51.100.7", loginForm("anna@example.com", testPassword)), http.StatusTooManyRequests, "Te veel")
	// The IP key blocks other accounts from the same address.
	wantStatus(t, from("192.0.2.1", loginForm("bert", testPassword)), http.StatusTooManyRequests, "Te veel")
	// Another user from another address is free.
	wantRedirect(t, from("198.51.100.7", loginForm("bert", testPassword)), "/")

	// Behind Tailscale Funnel every request comes from loopback; the client
	// is the last X-Forwarded-For entry.
	for i := range 5 {
		r := app.request("POST", "/login", loginForm("nobody", "x"+strconv.Itoa(i)), nil)
		r.RemoteAddr = "127.0.0.1:40000"
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		app.serve(r)
	}
	r := app.request("POST", "/login", loginForm("bert", testPassword), nil)
	r.RemoteAddr = "127.0.0.1:40000"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	wantStatus(t, app.serve(r), http.StatusTooManyRequests, "Te veel")
	r = app.request("POST", "/login", loginForm("bert", testPassword), nil)
	r.RemoteAddr = "127.0.0.1:40000"
	r.Header.Set("X-Forwarded-For", "203.0.113.10")
	wantRedirect(t, app.serve(r), "/")
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		remote string
		xff    []string
		want   string
	}{
		{"192.0.2.1:1234", nil, "192.0.2.1"},
		{"192.0.2.1:1234", []string{"203.0.113.9"}, "192.0.2.1"}, // not from the proxy: ignored
		{"127.0.0.1:1234", nil, "127.0.0.1"},
		{"127.0.0.1:1234", []string{"203.0.113.9"}, "203.0.113.9"},
		{"127.0.0.1:1234", []string{"6.6.6.6, 203.0.113.9"}, "203.0.113.9"}, // a client can prepend anything
		{"127.0.0.1:1234", []string{"6.6.6.6", "203.0.113.9"}, "203.0.113.9"},
		{"127.0.0.1:1234", []string{"nonsense"}, "127.0.0.1"},
		{"[::1]:1234", []string{"2001:db8::1"}, "2001:db8::1"},
		{"[::ffff:127.0.0.1]:1234", []string{"::ffff:203.0.113.9"}, "203.0.113.9"},
		{"[2001:db8::2]:1234", nil, "2001:db8::2"},
		{"garbage", nil, "garbage"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remote
		for _, v := range tt.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := clientIP(r); got != tt.want {
			t.Errorf("clientIP(%s, %q) = %s, want %s", tt.remote, tt.xff, got, tt.want)
		}
	}
}

func TestCrossOriginRefused(t *testing.T) {
	app := newTestApp(t, config{})
	app.addUser("anna", false)
	tests := []struct {
		name           string
		secFetchSite   string
		origin         string
		wantStatusCode int
	}{
		{"other site", "cross-site", "https://evil.example", http.StatusForbidden},
		// nestje on port 443 of the same host is same-site for the browser.
		{"same site, other port", "same-site", "http://example.com:8080", http.StatusForbidden},
		{"old browser, other origin", "", "https://evil.example", http.StatusForbidden},
		{"old browser, other port", "", "http://example.com:8080", http.StatusForbidden},
		{"same origin", "same-origin", "http://example.com", http.StatusSeeOther},
		{"old browser, same origin", "", "http://example.com", http.StatusSeeOther},
		{"no browser (curl)", "", "", http.StatusSeeOther},
	}
	for _, tt := range tests {
		// Every state-changing route, logged in or not, goes through the check.
		for _, path := range []string{"/login", "/logout-all"} {
			cookie := app.session(1)
			r := app.request("POST", path, loginForm("anna", testPassword), cookie)
			r.Header.Del("Sec-Fetch-Site")
			if tt.secFetchSite != "" {
				r.Header.Set("Sec-Fetch-Site", tt.secFetchSite)
			}
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			rec := app.serve(r)
			if rec.Code != tt.wantStatusCode {
				t.Errorf("%s: POST %s = %d, want %d", tt.name, path, rec.Code, tt.wantStatusCode)
			}
			if tt.wantStatusCode == http.StatusForbidden && responseCookie(rec) != nil {
				t.Errorf("%s: POST %s set a cookie", tt.name, path)
			}
		}
	}
}

func TestRegister(t *testing.T) {
	app := newTestApp(t, config{})
	adminID := app.addUser("anna", true)
	form := func(name, email, pw, pw2 string) url.Values {
		return url.Values{"username": {name}, "email": {email}, "password": {pw}, "password2": {pw2}}
	}

	// Closed: the page doesn't exist and the login page doesn't link to it.
	wantStatus(t, app.do("GET", "/register", nil, nil), http.StatusNotFound, "Pagina niet gevonden")
	wantStatus(t, app.do("POST", "/register", form("bert", "bert@example.com", "geheim1", "geheim1"), nil),
		http.StatusNotFound, "Pagina niet gevonden")
	if body := app.do("GET", "/login", nil, nil).Body.String(); strings.Contains(body, `href="/register"`) {
		t.Error("login page links to the closed registration")
	}

	if err := auth.SetRegistrationOpen(t.Context(), app.db, true); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, app.do("GET", "/register", nil, nil), http.StatusOK, "Account aanmaken")
	wantStatus(t, app.do("GET", "/login", nil, nil), http.StatusOK, `href="/register"`)
	wantRedirect(t, app.do("GET", "/register", nil, app.session(adminID)), "/")

	refused := []struct {
		name string
		form url.Values
		want string
	}{
		{"passwords differ", form("bert", "bert@example.com", "geheim1", "geheim2"), "De wachtwoorden zijn niet gelijk."},
		{"short password", form("bert", "bert@example.com", "kort", "kort"), "minstens 6 tekens"},
		{"bad username", form("bert de vries", "bert@example.com", "geheim1", "geheim1"), "Gebruikersnaam: 3–32 tekens"},
		{"bad email", form("bert", "bert", "geheim1", "geheim1"), "Ongeldig e-mailadres."},
		{"name taken", form("Anna", "bert@example.com", "geheim1", "geheim1"), "al in gebruik"},
		{"email taken", form("bert", "ANNA@example.com", "geheim1", "geheim1"), "al in gebruik"},
	}
	for _, tt := range refused {
		rec := app.do("POST", "/register", tt.form, nil)
		wantStatus(t, rec, http.StatusBadRequest, tt.want)
		if responseCookie(rec) != nil {
			t.Errorf("%s: cookie set", tt.name)
		}
		// Typed values come back, except the passwords.
		if !strings.Contains(rec.Body.String(), `value="`+tt.form.Get("username")+`"`) {
			t.Errorf("%s: username not filled in again", tt.name)
		}
	}

	rec := app.do("POST", "/register", form(" bert ", "bert@example.com", "geheim1", "geheim1"), nil)
	wantRedirect(t, rec, "/")
	c := responseCookie(rec)
	if c == nil {
		t.Fatal("registration didn't log in")
	}
	wantStatus(t, app.do("GET", "/api/me", nil, c), http.StatusOK, `{"isAdmin":false,"username":"bert"}`)
}

func TestLogout(t *testing.T) {
	app := newTestApp(t, config{})
	id := app.addUser("anna", false)
	cookie, other := app.session(id), app.session(id)

	rec := app.do("POST", "/logout", nil, cookie)
	wantRedirect(t, rec, "/login")
	if c := responseCookie(rec); c == nil || c.MaxAge >= 0 {
		t.Errorf("logout cookie = %+v, want one that deletes it", c)
	}
	wantRedirect(t, app.do("GET", "/", nil, cookie), "/login")
	wantStatus(t, app.do("GET", "/", nil, other), http.StatusOK, "Leitner") // the other device stays in

	// Without (valid) session it still clears the cookie.
	wantRedirect(t, app.do("POST", "/logout", nil, nil), "/login")
	wantRedirect(t, app.do("POST", "/logout", nil, cookie), "/login")
}

func TestLogoutAll(t *testing.T) {
	app := newTestApp(t, config{})
	anna, bert := app.addUser("anna", false), app.addUser("bert", false)
	a1, a2, b1 := app.session(anna), app.session(anna), app.session(bert)

	rec := app.do("POST", "/logout-all", nil, a1)
	wantRedirect(t, rec, "/login")
	if c := responseCookie(rec); c == nil || c.MaxAge >= 0 {
		t.Errorf("cookie = %+v, want one that deletes it", c)
	}
	for _, c := range []*http.Cookie{a1, a2} {
		wantStatus(t, app.do("GET", "/api/me", nil, c), http.StatusUnauthorized, "Niet ingelogd")
	}
	wantStatus(t, app.do("GET", "/api/me", nil, b1), http.StatusOK, "bert")
}

func TestAPIMe(t *testing.T) {
	app := newTestApp(t, config{})
	for _, admin := range []bool{false, true} {
		name := "user" + strconv.FormatBool(admin)
		rec := app.do("GET", "/api/me", nil, app.session(app.addUser(name, admin)))
		if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("headers = %v", rec.Header())
		}
		var me struct {
			Username string `json:"username"`
			IsAdmin  bool   `json:"isAdmin"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
			t.Fatal(err)
		}
		if me.Username != name || me.IsAdmin != admin {
			t.Errorf("me = %+v", me)
		}
	}
}

func TestPagesNoStore(t *testing.T) {
	app := newTestApp(t, config{})
	cookie := app.session(app.addUser("anna", false))
	for _, rec := range []*httptest.ResponseRecorder{
		app.do("GET", "/login", nil, nil),
		app.do("GET", "/missing", nil, cookie),
	} {
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
	}
}

func TestLayoutMenu(t *testing.T) {
	app := newTestApp(t, config{})
	user := app.session(app.addUser("anna", false))
	admin := app.session(app.addUser("bert", true))

	body := app.do("GET", "/missing", nil, user).Body.String()
	if !strings.Contains(body, "Ingelogd als anna") || !strings.Contains(body, `action="/logout"`) {
		t.Errorf("menu missing for a logged-in user:\n%s", body)
	}
	if strings.Contains(body, `href="/admin"`) {
		t.Error("non-admin sees the Admin link")
	}
	if body := app.do("GET", "/missing", nil, admin).Body.String(); !strings.Contains(body, `href="/admin"`) {
		t.Error("admin misses the Admin link")
	}
	if body := app.do("GET", "/login", nil, nil).Body.String(); strings.Contains(body, "Ingelogd als") {
		t.Error("menu shown to a logged-out visitor")
	}
}
