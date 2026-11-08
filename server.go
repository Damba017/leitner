package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"time"
)

type config struct {
	addr    string
	dbPath  string
	baseURL string // public https URL; used by Web Push (phase 9)
}

func init() {
	// Not in Go's built-in table, and the OS mime files may lack it.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

type server struct {
	db    *sql.DB
	pages pages
}

func newHandler(db *sql.DB) http.Handler {
	s := &server{db: db, pages: mustParsePages()}

	mux := http.NewServeMux()
	mux.Handle("GET /", s.static(webFS()))
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// frame-ancestors, base-uri and form-action don't fall back to default-src.
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		// Not no-referrer: that makes browsers send "Origin: null" on form
		// POSTs, which would break an Origin check on state-changing requests.
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func serve(ctx context.Context, cfg config, db *sql.DB) error {
	nightly := newNightlyBackups(cfg.dbPath)
	go nightly.run(ctx)

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           newHandler(db),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}

	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return err
	}
	slog.Info("listening", "addr", ln.Addr().String(), "db", cfg.dbPath, "version", version())

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.Info("stopped")
	return nil
}
