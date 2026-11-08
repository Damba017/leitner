package main

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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

func TestStatic(t *testing.T) {
	h := newHandler(openTestDB(t))
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
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			res := rec.Result()
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
	h := newHandler(openTestDB(t))
	for _, path := range []string{"/", "/missing"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		for name, want := range map[string]string{
			"Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "same-origin",
		} {
			if got := rec.Header().Get(name); got != want {
				t.Errorf("%s: %s = %q, want %q", path, name, got, want)
			}
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newHandler(openTestDB(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d, want 405", rec.Code)
	}
}

func TestPagesParse(t *testing.T) {
	p := mustParsePages()
	if _, ok := p["notfound.html"]; !ok {
		t.Errorf("pages = %v, want notfound.html", p)
	}
	if _, ok := p["layout.html"]; ok {
		t.Error("layout.html is parsed as a page of its own")
	}
}

func TestRenderUnknownPage(t *testing.T) {
	s := &server{pages: mustParsePages()}
	rec := httptest.NewRecorder()
	s.render(rec, http.StatusOK, "nope.html", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
