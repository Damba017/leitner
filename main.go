// Command leitner is a self-hosted flashcard app with Leitner spaced repetition.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	_ "time/tzdata" // don't depend on the server's zoneinfo

	"github.com/Damba017/leitner/internal/store"
)

const usage = `Usage:
  leitner serve [-addr host:port] [-db file] [-url base-url] [-dev]
  leitner create-admin -db file                       (asks for name, email and password)
  leitner reset-password -db file username-or-email   (asks for the new password)
  leitner list-users -db file
  leitner backup -db file path
  leitner version

Run "leitner <command> -h" for the flags of a command.
`

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "serve":
		err = runServe(args)
	case "create-admin":
		err = runCreateAdmin(args, stdTerminal())
	case "reset-password":
		err = runResetPassword(args, stdTerminal())
	case "list-users":
		err = runListUsers(args, stdTerminal())
	case "backup":
		err = runBackup(args)
	case "version":
		err = runVersion(args, os.Stdout)
	case "-h", "-help", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "leitner:", err)
		os.Exit(1)
	}
}

// parseServe reads the serve flags; split from runServe so tests can check them.
func parseServe(args []string) (config, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var cfg config
	fs.StringVar(&cfg.addr, "addr", "127.0.0.1:8370", "listen address")
	fs.StringVar(&cfg.dbPath, "db", "leitner.db", "SQLite database file (its folder is created if missing)")
	fs.StringVar(&cfg.baseURL, "url", "", "public base URL, e.g. https://leitner.example.com:8443 (needed to send notifications)")
	fs.BoolVar(&cfg.dev, "dev", false, "local testing: session cookie without Secure, also with -url (never in production)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("serve: unexpected arguments %q", fs.Args())
	}
	// The push services only accept an https URL as contact (VAPID subject).
	if u, err := url.Parse(cfg.baseURL); cfg.baseURL != "" && (err != nil || u.Scheme != "https" || u.Host == "") {
		return config{}, errors.New("serve: -url must be an https URL, like https://leitner.example.com:8443")
	}
	return cfg, nil
}

func runServe(args []string) error {
	cfg, err := parseServe(args)
	if err != nil {
		return err
	}
	// A fresh checkout has no data/ folder yet; SQLite won't make it.
	if err := os.MkdirAll(filepath.Dir(cfg.dbPath), 0o700); err != nil {
		return fmt.Errorf("serve: create database folder: %w", err)
	}
	db, err := store.Open(cfg.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, cfg, db)
}

func runBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dbPath := fs.String("db", "leitner.db", "SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("backup: give one new file to write to, e.g. leitner backup -db leitner.db backup.db")
	}
	if err := store.Backup(context.Background(), *dbPath, fs.Arg(0)); err != nil {
		return err
	}
	fmt.Printf("Backup written to %s.\n", fs.Arg(0))
	return nil
}

func runVersion(args []string, w io.Writer) error {
	if len(args) > 0 {
		return fmt.Errorf("version: unexpected arguments %q", args)
	}
	fmt.Fprintln(w, "leitner", version())
	return nil
}

// version comes from the build info Go embeds: the module version (the git
// tag for a release build, "(devel)" otherwise) and the commit.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	return formatVersion(info)
}

func formatVersion(info *debug.BuildInfo) string {
	v := info.Main.Version
	if v == "" {
		v = "(devel)"
	}
	var rev string
	var dirty bool
	for _, kv := range info.Settings {
		switch kv.Key {
		case "vcs.revision":
			rev = kv.Value
		case "vcs.modified":
			dirty = kv.Value == "true"
		}
	}
	if rev == "" {
		return v
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-dirty"
	}
	return v + " (" + rev + ")"
}
