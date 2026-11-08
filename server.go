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

	"github.com/Damba017/leitner/internal/auth"
)

type config struct {
	addr    string
	dbPath  string
	baseURL string // public https URL: Secure cookies now, Web Push in phase 9
}

func init() {
	// Not in Go's built-in table, and the OS mime files may lack it.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

type server struct {
	db      *sql.DB
	pages   pages
	limiter *auth.Limiter // failed password checks, per IP and per user
	// secureCookies marks the session cookie Secure. -url is https-only and
	// always set in production; without it (local http) Secure would break login.
	secureCookies bool
	publicFiles   map[string]bool
}

func newHandler(db *sql.DB, cfg config) http.Handler {
	web := webFS()
	s := &server{
		db:            db,
		pages:         mustParsePages(),
		limiter:       auth.NewLimiter(5, 15*time.Minute),
		secureCookies: cfg.baseURL != "",
		publicFiles:   publicFiles(web),
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", s.static(web))
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("GET /register", s.registerPage)
	mux.HandleFunc("POST /register", s.register)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("POST /logout-all", s.logoutAll)
	mux.HandleFunc("GET /api/me", s.apiMe)
	mux.HandleFunc("GET /api/tree", s.apiTree)
	mux.HandleFunc("POST /api/headings", s.apiCreateHeading)
	mux.HandleFunc("PUT /api/headings/{id}", s.apiUpdateHeading)
	mux.HandleFunc("DELETE /api/headings/{id}", s.apiDeleteHeading)
	mux.HandleFunc("POST /api/lists", s.apiCreateList)
	mux.HandleFunc("PUT /api/lists/{id}", s.apiUpdateList)
	mux.HandleFunc("DELETE /api/lists/{id}", s.apiDeleteList)
	mux.HandleFunc("PUT /api/order", s.apiOrder)
	mux.HandleFunc("GET /account", s.accountPage)
	mux.HandleFunc("POST /account/password", s.changePassword)
	mux.Handle("GET /admin", s.adminOnly(s.adminPage))
	mux.Handle("POST /admin/registration", s.adminOnly(s.setRegistration))
	mux.Handle("POST /admin/users/{id}/admin", s.adminOnly(s.setUserAdmin))
	mux.Handle("POST /admin/users/{id}/password", s.adminOnly(s.resetUserPassword))
	mux.Handle("GET /admin/users/{id}/delete", s.adminOnly(s.confirmDelete))
	mux.Handle("POST /admin/users/{id}/delete", s.adminOnly(s.deleteUser))
	return securityHeaders(crossOriginProtection().Handler(s.requireSession(mux)))
}

// crossOriginProtection refuses state-changing requests from other origins
// (CSRF). It goes by Sec-Fetch-Site, so it also stops nestje on port 443 of
// the same host: browsers count that as same-site, so SameSite cookies alone
// wouldn't. Without that header it compares Origin with Host.
func crossOriginProtection() *http.CrossOriginProtection {
	c := http.NewCrossOriginProtection()
	c.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Warn("cross-origin request refused", "method", r.Method, "path", r.URL.Path,
			"origin", r.Header.Get("Origin"), "sec_fetch_site", r.Header.Get("Sec-Fetch-Site"), "ip", clientIP(r))
		http.Error(w, "Verboden", http.StatusForbidden)
	}))
	return c
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// frame-ancestors, base-uri and form-action don't fall back to default-src.
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		// Not no-referrer: that makes browsers send "Origin: null" on form
		// POSTs, which would break the Origin fallback of crossOriginProtection.
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func serve(ctx context.Context, cfg config, db *sql.DB) error {
	nightly := newNightlyBackups(cfg.dbPath)
	go nightly.run(ctx)

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           newHandler(db, cfg),
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
