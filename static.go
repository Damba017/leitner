package main

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
)

// Go's built-in table lacks .webmanifest, and the Arch laptop may have no
// /etc/mime.types either.
func init() {
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		panic(err)
	}
}

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

// publicFiles lists every static file except the app page itself, so styles
// and scripts (later icons and the manifest) also load on the login page.
func publicFiles(files fs.FS) map[string]bool {
	public := map[string]bool{}
	err := fs.WalkDir(files, ".", func(name string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && name != "index.html" {
			public["/"+name] = true
		}
		return err
	})
	if err != nil {
		panic(err) // the embedded files are fixed at compile time
	}
	return public
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
