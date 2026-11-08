package main

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"

	"github.com/Damba017/leitner/internal/auth"
)

//go:embed templates
var templateFiles embed.FS

// pages holds every server-rendered page, each parsed together with layout.html.
type pages map[string]*template.Template

// pageData is what every server-rendered page gets; each page uses a part.
type pageData struct {
	Me     auth.User // the logged-in user; zero on pages for logged-out visitors
	Nav    string    // menu item of this page ("account", "admin"), marked aria-current
	Error  string
	Notice string

	Login            string // login form: what was typed
	Username, Email  string // registration form: what was typed
	RegistrationOpen bool
	Users            []auth.User // admin page
	Confirm          confirmPage
}

// confirmPage asks before a destructive action; its button posts to Action.
// A page of its own instead of JS confirm(), which didn't always show in
// Safari (nestje).
type confirmPage struct {
	Question, Note, Action, Button, Back string
}

// mustParsePages parses all templates at start, so a broken one stops the
// server right away (and fails the tests) instead of a request later.
func mustParsePages() pages {
	names, err := fs.Glob(templateFiles, "templates/*.html")
	if err != nil {
		panic(err)
	}
	p := pages{}
	for _, name := range names {
		base := path.Base(name)
		if base == "layout.html" {
			continue
		}
		p[base] = template.Must(template.ParseFS(templateFiles, "templates/layout.html", name))
	}
	return p
}

// render writes the page with status. It renders into a buffer first, so a
// template error gives a clean 500 instead of half a page. Pages show
// account data, so no cache (browser or, later, service worker) keeps them.
func (s *server) render(w http.ResponseWriter, status int, name string, data pageData) {
	t, ok := s.pages[name]
	if !ok {
		slog.Error("render: unknown page", "page", name)
		http.Error(w, "Interne fout", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		slog.Error("render", "page", name, "err", err)
		http.Error(w, "Interne fout", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	s.render(w, http.StatusNotFound, "notfound.html", pageData{Me: me})
}

func (s *server) serverError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	http.Error(w, "Er ging iets mis op de server.", http.StatusInternalServerError)
}
