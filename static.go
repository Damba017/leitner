package main

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed web
var webFiles embed.FS

// webFS is the embedded web/ folder as the root of the site.
func webFS() fs.FS {
	files, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err) // the embed path is fixed at compile time
	}
	return files
}

// static serves the app shell from files. Unknown paths and folders get the
// templated 404 page instead of http.FileServer's plain text or a listing.
func (s *server) static(files fs.FS) http.Handler {
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(r.URL.Path)
		if name != "/" {
			fi, err := fs.Stat(files, name[1:])
			if err != nil || fi.IsDir() {
				s.notFound(w, r)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
