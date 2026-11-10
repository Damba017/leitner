package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
)

// serviceWorker serves web/sw.js with a header naming the build version and
// the files to cache. The version is a hash of every embedded web file, so
// any change makes the browser see a new worker, which fills a new cache and
// drops the old one.
func serviceWorker(static fs.FS) http.Handler {
	body, err := fs.ReadFile(static, "sw.js")
	if err != nil {
		panic(err) // the embed path is fixed at compile time
	}
	sum := sha256.New()
	files := []string{"/"} // the app page is index.html, served at /
	err = fs.WalkDir(static, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(static, path)
		if err != nil {
			return err
		}
		sum.Write([]byte(path))
		sum.Write(data)
		if path != "index.html" && path != "sw.js" {
			files = append(files, "/"+path)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	list, err := json.Marshal(files)
	if err != nil {
		panic(err)
	}
	script := []byte("const VERSION = \"" + hex.EncodeToString(sum.Sum(nil))[:16] + "\";\nconst APP_FILES = " + string(list) + ";\n" + string(body))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		// The browser must look for a new worker on every visit.
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(script)
	})
}
