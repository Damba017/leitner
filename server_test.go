package main

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Damba017/leitner/internal/auth"
	"github.com/Damba017/leitner/internal/store"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "leitner.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// testApp is the whole handler over a fresh database.
type testApp struct {
	t  *testing.T
	db *sql.DB
	s  *server
	h  http.Handler
}

func newTestApp(t *testing.T, cfg config) *testApp {
	t.Helper()
	db := openTestDB(t)
	push, err := newPushSender(t.Context(), db, cfg.baseURL)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(db, cfg, push)
	return &testApp{t: t, db: db, s: s, h: s.handler()}
}

const testPassword = "geheim1"

// addUser creates name with email name@example.com and testPassword.
func (a *testApp) addUser(name string, admin bool) int64 {
	a.t.Helper()
	id, err := auth.CreateUser(context.Background(), a.db,
		auth.NewUser{Username: name, Email: name + "@example.com", Password: testPassword, IsAdmin: admin})
	if err != nil {
		a.t.Fatal(err)
	}
	return id
}

// session logs userID in without the login form and returns the cookie.
func (a *testApp) session(userID int64) *http.Cookie {
	a.t.Helper()
	token, err := auth.NewSession(context.Background(), a.db, userID, time.Now())
	if err != nil {
		a.t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookie, Value: token}
}

// request is built like a browser on the app's own page sends it: with
// Sec-Fetch-Site same-origin. form (may be nil) makes it a form post.
func (a *testApp) request(method, path string, form url.Values, c *http.Cookie) *http.Request {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

func (a *testApp) serve(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, r)
	return rec
}

func (a *testApp) do(method, path string, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	return a.serve(a.request(method, path, form, c))
}

// wantRedirect fails unless rec is a 303 to location.
func wantRedirect(t *testing.T, rec *httptest.ResponseRecorder, location string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != location {
		t.Errorf("got %d to %q, want 303 to %q\n%s", rec.Code, rec.Header().Get("Location"), location, rec.Body)
	}
}

// wantStatus fails unless rec has status code and its body contains body.
func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, code int, body string) {
	t.Helper()
	if rec.Code != code {
		t.Errorf("status = %d, want %d\n%s", rec.Code, code, rec.Body)
	} else if !strings.Contains(rec.Body.String(), body) {
		t.Errorf("body lacks %q:\n%s", body, rec.Body)
	}
}

func TestStatic(t *testing.T) {
	app := newTestApp(t, config{})
	cookie := app.session(app.addUser("anna", false))
	tests := []struct {
		path       string
		wantStatus int
		wantType   string
		wantBody   string
	}{
		{"/", http.StatusOK, "text/html; charset=utf-8", "<title>Leitner</title>"},
		{"/index.html", http.StatusMovedPermanently, "", ""}, // FileServer redirects to /
		{"/app.css", http.StatusOK, "text/css; charset=utf-8", "--accent"},
		{"/app.js", http.StatusOK, "text/javascript; charset=utf-8", "ES module"},
		{"/missing", http.StatusNotFound, "text/html; charset=utf-8", "Pagina niet gevonden"},
		{"/deep/missing/page", http.StatusNotFound, "text/html; charset=utf-8", `href="/app.css"`},
		{"/main.go", http.StatusNotFound, "text/html; charset=utf-8", "Pagina niet gevonden"}, // only web/ is served
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			res := app.do("GET", tt.path, nil, cookie).Result()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if tt.wantType != "" && res.Header.Get("Content-Type") != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", res.Header.Get("Content-Type"), tt.wantType)
			}
			if !strings.Contains(string(body), tt.wantBody) {
				t.Errorf("body lacks %q:\n%s", tt.wantBody, body)
			}
		})
	}
}

// Folders must not be listed. web/ has none yet (icons come in phase 7), so
// the check runs against a file system made for the test.
func TestStaticNoDirectoryListing(t *testing.T) {
	s := &server{pages: mustParsePages()}
	files := fstest.MapFS{
		"index.html":       {Data: []byte("app")},
		"icons/icon.svg":   {Data: []byte("<svg/>")},
		"icons/index.html": {Data: []byte("not the app")},
	}
	h := s.static(files)
	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/", http.StatusOK},
		{"/icons/icon.svg", http.StatusOK},
		{"/icons", http.StatusNotFound},
		{"/icons/", http.StatusNotFound},
		{"/icons/.", http.StatusNotFound},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tt.path, nil))
		if rec.Code != tt.wantStatus {
			t.Errorf("%s = %d, want %d", tt.path, rec.Code, tt.wantStatus)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	app := newTestApp(t, config{})
	cookie := app.session(app.addUser("anna", false))
	// Also on redirects, refusals and the pages for logged-out visitors.
	tests := []struct {
		method, path string
		cookie       *http.Cookie
	}{
		{"GET", "/", cookie},
		{"GET", "/missing", cookie},
		{"GET", "/", nil},
		{"GET", "/login", nil},
		{"POST", "/api/me", nil},
	}
	for _, tt := range tests {
		rec := app.do(tt.method, tt.path, nil, tt.cookie)
		for name, want := range map[string]string{
			"Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "same-origin",
		} {
			if got := rec.Header().Get(name); got != want {
				t.Errorf("%s %s (logged in %v): %s = %q, want %q", tt.method, tt.path, tt.cookie != nil, name, got, want)
			}
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	app := newTestApp(t, config{})
	cookie := app.session(app.addUser("anna", false))
	if rec := app.do("POST", "/", nil, cookie); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d, want 405", rec.Code)
	}
}

func TestPagesParse(t *testing.T) {
	p := mustParsePages()
	for _, name := range []string{"notfound.html", "login.html", "register.html", "account.html", "admin.html", "confirm.html"} {
		if _, ok := p[name]; !ok {
			t.Errorf("pages = %v, want %s", p, name)
		}
	}
	if _, ok := p["layout.html"]; ok {
		t.Error("layout.html is parsed as a page of its own")
	}
}

func TestRenderUnknownPage(t *testing.T) {
	s := &server{pages: mustParsePages()}
	rec := httptest.NewRecorder()
	s.render(rec, http.StatusOK, "nope.html", pageData{})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// The manifest, service worker and icons must load without a session (the
// login page links them) and with the right content types.
func TestPWAFiles(t *testing.T) {
	app := newTestApp(t, config{})
	for path, wantType := range map[string]string{
		"/manifest.webmanifest":        "application/manifest+json",
		"/sw.js":                       "text/javascript",
		"/pwa.js":                      "text/javascript",
		"/icon.svg":                    "image/svg+xml",
		"/icons/icon-192.png":          "image/png",
		"/icons/icon-512.png":          "image/png",
		"/icons/icon-maskable-512.png": "image/png",
		"/icons/apple-touch-icon.png":  "image/png",
	} {
		rec := app.do("GET", path, nil, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, wantType) {
			t.Errorf("%s Content-Type = %q, want %q", path, got, wantType)
		}
	}
}
