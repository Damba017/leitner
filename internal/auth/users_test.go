package auth

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Damba017/leitner/internal/store"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "leitner.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustCreate(t *testing.T, db *sql.DB, name string, admin bool) int64 {
	t.Helper()
	id, err := CreateUser(context.Background(), db,
		NewUser{Username: name, Email: name + "@example.com", Password: "geheim1", IsAdmin: admin})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"abc", true},
		{"Anna.de_Vries-2", true},
		{strings.Repeat("a", 32), true},
		{"ab", false},
		{strings.Repeat("a", 33), false},
		{"anna@home", false},
		{"anna vries", false},
		{"anna!", false},
		{"änna", false},
		{"", false},
	}
	for _, tt := range tests {
		if err := ValidateUsername(tt.name); (err == nil) != tt.ok {
			t.Errorf("ValidateUsername(%q) = %v, want ok %v", tt.name, err, tt.ok)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email string
		ok    bool
	}{
		{"a@b.nl", true},
		{"anna.de.vries+leitner@example.com", true},
		{"", false},
		{"anna", false},
		{"Anna <anna@example.com>", false},
		{"anna@example.com ", false},
		{strings.Repeat("a", 250) + "@b.nl", false},
	}
	for _, tt := range tests {
		if err := ValidateEmail(tt.email); (err == nil) != tt.ok {
			t.Errorf("ValidateEmail(%q) = %v, want ok %v", tt.email, err, tt.ok)
		}
	}
}

func TestCreateUser(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	id, err := CreateUser(ctx, db, NewUser{Username: "  anna ", Email: " anna@example.com ", Password: "geheim1"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := GetUser(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if u != (User{ID: id, Username: "anna", Email: "anna@example.com"}) {
		t.Errorf("user = %+v (trimmed, not admin)", u)
	}

	tests := []struct {
		name string
		nu   NewUser
		want error
	}{
		{"same name", NewUser{"anna", "other@example.com", "geheim1", false}, ErrUserExists},
		{"name other case", NewUser{"ANNA", "other@example.com", "geheim1", false}, ErrUserExists},
		{"email other case", NewUser{"bert", "Anna@Example.COM", "geheim1", false}, ErrUserExists},
		{"bad name", NewUser{"a b", "bert@example.com", "geheim1", false}, ErrInvalidUsername},
		{"bad email", NewUser{"bert", "bert", "geheim1", false}, ErrInvalidEmail},
		{"short password", NewUser{"bert", "bert@example.com", "12345", false}, ErrPasswordTooShort},
	}
	for _, tt := range tests {
		if _, err := CreateUser(ctx, db, tt.nu); err != tt.want {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	users, err := ListUsers(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Errorf("users = %+v, want only anna", users)
	}
}

func TestFindForLogin(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	id := mustCreate(t, db, "anna", true)

	for _, login := range []string{"anna", "ANNA", "anna@example.com", "Anna@Example.com"} {
		u, hash, err := FindForLogin(ctx, db, login)
		if err != nil {
			t.Fatalf("%q: %v", login, err)
		}
		if u.ID != id || !u.IsAdmin || u.Username != "anna" {
			t.Errorf("%q: user = %+v", login, u)
		}
		if ok, err := VerifyPassword(ctx, hash, "geheim1"); !ok || err != nil {
			t.Errorf("%q: hash doesn't verify: %v %v", login, ok, err)
		}
	}
	// A username is never looked up as an email and the other way round.
	for _, login := range []string{"bert", "", "example.com", "anna@", "@example.com"} {
		if _, _, err := FindForLogin(ctx, db, login); err != ErrUserNotFound {
			t.Errorf("%q: err = %v, want ErrUserNotFound", login, err)
		}
	}
	if _, err := GetUser(ctx, db, id+1); err != ErrUserNotFound {
		t.Errorf("GetUser(unknown) = %v", err)
	}
	hash, err := PasswordHash(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword(ctx, hash, "geheim1"); !ok || err != nil {
		t.Errorf("PasswordHash doesn't verify: %v %v", ok, err)
	}
	if _, err := PasswordHash(ctx, db, id+1); err != ErrUserNotFound {
		t.Errorf("PasswordHash(unknown) = %v", err)
	}
}

func TestListUsersOrder(t *testing.T) {
	db := testDB(t)
	for _, n := range []string{"carla", "anna", "Bert"} {
		mustCreate(t, db, n, false)
	}
	users, err := ListUsers(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range users {
		names = append(names, u.Username)
	}
	if got := strings.Join(names, ","); got != "anna,Bert,carla" {
		t.Errorf("order = %s", got)
	}
}

func TestRegistrationOpen(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	for i, want := range []bool{false, true, true, false} {
		if i > 0 {
			if err := SetRegistrationOpen(ctx, db, want); err != nil {
				t.Fatal(err)
			}
		}
		got, err := RegistrationOpen(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("step %d: open = %v, want %v", i, got, want)
		}
	}
}
