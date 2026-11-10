package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"testing"
)

// The service worker gets the build version and the files to cache in front
// of web/sw.js, and the browser must check it for updates on every visit.
func TestServiceWorker(t *testing.T) {
	app := newTestApp(t, config{})
	rec := app.do("GET", "/sw.js", nil, nil)
	wantStatus(t, rec, http.StatusOK, "self.addEventListener(\"fetch\"")
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q", got)
	}
	m := regexp.MustCompile(`^const VERSION = "([0-9a-f]{16})";\nconst APP_FILES = (\[.*\]);\n`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("no VERSION/APP_FILES header:\n%.200s", rec.Body)
	}
	var files []string
	if err := json.Unmarshal([]byte(m[2]), &files); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/", "/app.js", "/app.css", "/manifest.webmanifest", "/icons/icon-192.png"} {
		if !slices.Contains(files, want) {
			t.Errorf("APP_FILES lacks %s: %v", want, files)
		}
	}
	for _, unwanted := range []string{"/index.html", "/sw.js"} {
		if slices.Contains(files, unwanted) {
			t.Errorf("APP_FILES has %s", unwanted)
		}
	}
	// Every listed file must exist and load without a session (the worker
	// installs from the login page too); "/" redirects there.
	for _, f := range files[1:] {
		if rec := app.do("GET", f, nil, nil); rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", f, rec.Code)
		}
	}
}
