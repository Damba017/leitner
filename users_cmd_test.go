package main

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Damba017/leitner/internal/auth"
	"github.com/Damba017/leitner/internal/store"
)

// fakeTerminal answers the prompts with the given lines, one per prompt.
func fakeTerminal(lines ...string) (*terminal, *strings.Builder) {
	out := &strings.Builder{}
	in := strings.Join(lines, "\n")
	if len(lines) > 0 {
		in += "\n"
	}
	return &terminal{in: bufio.NewReader(strings.NewReader(in)), out: out}, out
}

func TestCreateAdmin(t *testing.T) {
	// A fresh install: neither the folder nor the database exists yet.
	path := filepath.Join(t.TempDir(), "data", "leitner.db")
	term, out := fakeTerminal(" anna ", "anna@example.com", "geheim1", "geheim1")
	if err := runCreateAdmin([]string{"-db", path}, term); err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"Username: ", "Email: ", "New password: ", "Repeat password: ", "Created admin anna."} {
		if !strings.Contains(out.String(), prompt) {
			t.Errorf("output lacks %q:\n%s", prompt, out)
		}
	}
	if strings.Contains(out.String(), "geheim1") {
		t.Error("password printed")
	}

	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	u, hash, err := auth.FindForLogin(context.Background(), db, "anna")
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin || u.Email != "anna@example.com" {
		t.Errorf("user = %+v", u)
	}
	if ok, _ := auth.VerifyPassword(context.Background(), hash, "geheim1"); !ok {
		t.Error("password doesn't verify")
	}

	refused := []struct {
		name  string
		lines []string
		want  string
	}{
		{"bad username", []string{"a b"}, "username must be"},
		{"bad email", []string{"bert", "bert"}, "invalid email"},
		{"short password", []string{"bert", "bert@example.com", "kort"}, "at least 6"},
		{"passwords differ", []string{"bert", "bert@example.com", "geheim1", "geheim2"}, "don't match"},
		{"input ends", []string{"bert"}, "read input"},
		{"name taken", []string{"ANNA", "other@example.com", "geheim1", "geheim1"}, "already in use"},
	}
	for _, tt := range refused {
		term, _ := fakeTerminal(tt.lines...)
		err := runCreateAdmin([]string{"-db", path}, term)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
	users, err := auth.ListUsers(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Errorf("users = %+v, want only anna", users)
	}
}

func TestResetPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leitner.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	id, err := auth.CreateUser(ctx, db, auth.NewUser{Username: "anna", Email: "anna@example.com", Password: "geheim1"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.NewSession(ctx, db, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	term, out := fakeTerminal("nieuw-geheim", "nieuw-geheim")
	if err := runResetPassword([]string{"-db", path, "anna@example.com"}, term); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Password of anna changed; anna is logged out everywhere.") {
		t.Errorf("output:\n%s", out)
	}
	if _, _, err := auth.LookupSession(ctx, db, token, time.Now()); err != auth.ErrNoSession {
		t.Errorf("session after reset: %v", err)
	}
	hash, err := auth.PasswordHash(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := auth.VerifyPassword(ctx, hash, "nieuw-geheim"); !ok {
		t.Error("new password doesn't verify")
	}

	for _, tt := range []struct {
		name  string
		args  []string
		lines []string
		want  string
	}{
		{"unknown user", []string{"-db", path, "bert"}, nil, "user not found"},
		{"no user", []string{"-db", path}, nil, "give one username or email"},
		{"two users", []string{"-db", path, "anna", "bert"}, nil, "give one username or email"},
		{"short", []string{"-db", path, "anna"}, []string{"kort"}, "at least 6"},
		{"no database", []string{"-db", path + ".typo", "anna"}, nil, "doesn't exist"},
	} {
		term, _ := fakeTerminal(tt.lines...)
		err := runResetPassword(tt.args, term)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := os.Stat(path + ".typo"); !os.IsNotExist(err) {
		t.Error("reset-password created a database for a wrong path")
	}
}

func TestListUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leitner.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, nu := range []auth.NewUser{
		{Username: "bert", Email: "bert@example.com", Password: "geheim1"},
		{Username: "anna", Email: "anna@example.com", Password: "geheim1", IsAdmin: true},
	} {
		if _, err := auth.CreateUser(context.Background(), db, nu); err != nil {
			t.Fatal(err)
		}
	}
	term, out := fakeTerminal()
	if err := runListUsers([]string{"-db", path}, term); err != nil {
		t.Fatal(err)
	}
	want := "admin  anna" + strings.Repeat(" ", 28) + "  anna@example.com\n" +
		"user   bert" + strings.Repeat(" ", 28) + "  bert@example.com\n" +
		"2 users\n"
	if out.String() != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
	if err := runListUsers([]string{"-db", path, "extra"}, term); err == nil {
		t.Error("extra argument accepted")
	}
	if err := runListUsers([]string{"-db", path + ".typo"}, term); err == nil {
		t.Error("missing database accepted")
	}
}
