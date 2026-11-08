package main

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
)

//go:embed templates
var templateFiles embed.FS

// pages holds every server-rendered page, each parsed together with layout.html.
type pages map[string]*template.Template

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
// template error gives a clean 500 instead of half a page.
func (s *server) render(w http.ResponseWriter, status int, name string, data any) {
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
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, "notfound.html", nil)
}
