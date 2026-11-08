package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Damba017/leitner/internal/auth"
	"github.com/Damba017/leitner/internal/store"
)

// terminal is where the user commands ask their questions.
type terminal struct {
	in  *bufio.Reader
	out io.Writer
	// hideInput turns echo off while a password is typed; only for a real
	// terminal, not for piped input (or tests).
	hideInput bool
}

func stdTerminal() *terminal {
	fi, err := os.Stdin.Stat()
	return &terminal{
		in:        bufio.NewReader(os.Stdin),
		out:       os.Stdout,
		hideInput: err == nil && fi.Mode()&os.ModeCharDevice != 0,
	}
}

func (t *terminal) line(prompt string) (string, error) {
	fmt.Fprint(t.out, prompt)
	s, err := t.in.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// password reads a line with echo off, via stty (no extra module needed).
func (t *terminal) password(prompt string) (string, error) {
	if t.hideInput && stty("-echo") == nil {
		// Ctrl-C would otherwise leave the terminal without echo.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			select {
			case <-sig:
				stty("echo")
				fmt.Fprintln(t.out)
				os.Exit(130)
			case <-done:
			}
		}()
		defer func() {
			close(done)
			signal.Stop(sig)
			stty("echo")
			fmt.Fprintln(t.out) // the Enter wasn't echoed
		}()
	}
	return t.line(prompt)
}

// newPassword asks for a new password twice and checks it.
func (t *terminal) newPassword() (string, error) {
	pw, err := t.password("New password: ")
	if err != nil {
		return "", err
	}
	if err := auth.CheckPassword(pw); err != nil {
		return "", err
	}
	pw2, err := t.password("Repeat password: ")
	if err != nil {
		return "", err
	}
	if pw != pw2 {
		return "", errors.New("passwords don't match")
	}
	return pw, nil
}

func stty(args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// userCmd parses the "-db file" flag, opens the database and runs fn.
// Commands that act on one user take the username or email as the last
// argument. Only create may make a new database (create-admin on a fresh
// install); for the others a missing file is most likely a typo.
func userCmd(name string, args []string, wantUser, create bool, fn func(ctx context.Context, db *sql.DB, login string) error) error {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	dbPath := flags.String("db", "leitner.db", "SQLite database file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	login := ""
	switch {
	case wantUser && flags.NArg() != 1:
		return fmt.Errorf("%s: give one username or email, after the flags", name)
	case !wantUser && flags.NArg() > 0:
		return fmt.Errorf("%s: unexpected arguments %q", name, flags.Args())
	case wantUser:
		login = flags.Arg(0)
	}

	if create {
		if err := os.MkdirAll(filepath.Dir(*dbPath), 0o700); err != nil {
			return fmt.Errorf("%s: create database folder: %w", name, err)
		}
	} else if _, err := os.Stat(*dbPath); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: database %s doesn't exist", name, *dbPath)
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(context.Background(), db, login)
}

func runCreateAdmin(args []string, t *terminal) error {
	return userCmd("create-admin", args, false, true, func(ctx context.Context, db *sql.DB, _ string) error {
		username, err := t.line("Username: ")
		if err != nil {
			return err
		}
		username = strings.TrimSpace(username)
		if err := auth.ValidateUsername(username); err != nil {
			return err
		}
		email, err := t.line("Email: ")
		if err != nil {
			return err
		}
		email = strings.TrimSpace(email)
		if err := auth.ValidateEmail(email); err != nil {
			return err
		}
		pw, err := t.newPassword()
		if err != nil {
			return err
		}
		if _, err := auth.CreateUser(ctx, db, auth.NewUser{Username: username, Email: email, Password: pw, IsAdmin: true}); err != nil {
			return err
		}
		fmt.Fprintf(t.out, "Created admin %s.\n", username)
		return nil
	})
}

func runResetPassword(args []string, t *terminal) error {
	return userCmd("reset-password", args, true, false, func(ctx context.Context, db *sql.DB, login string) error {
		u, _, err := auth.FindForLogin(ctx, db, login)
		if err != nil {
			return err
		}
		fmt.Fprintf(t.out, "Resetting the password of %s (%s).\n", u.Username, u.Email)
		pw, err := t.newPassword()
		if err != nil {
			return err
		}
		if err := auth.SetPassword(ctx, db, u.ID, pw, ""); err != nil {
			return err
		}
		fmt.Fprintf(t.out, "Password of %s changed; %s is logged out everywhere.\n", u.Username, u.Username)
		return nil
	})
}

func runListUsers(args []string, t *terminal) error {
	return userCmd("list-users", args, false, false, func(ctx context.Context, db *sql.DB, _ string) error {
		users, err := auth.ListUsers(ctx, db)
		if err != nil {
			return err
		}
		for _, u := range users {
			role := "user "
			if u.IsAdmin {
				role = "admin"
			}
			fmt.Fprintf(t.out, "%s  %-32s  %s\n", role, u.Username, u.Email)
		}
		fmt.Fprintf(t.out, "%d users\n", len(users))
		return nil
	})
}
