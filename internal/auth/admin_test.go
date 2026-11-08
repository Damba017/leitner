package auth

import (
	"context"
	"testing"
	"time"
)

func TestSetAdmin(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	anna := mustCreate(t, db, "anna", true)
	bert := mustCreate(t, db, "bert", false)

	steps := []struct {
		name  string
		id    int64
		admin bool
		want  error
	}{
		{"revoke the only admin", anna, false, ErrLastAdmin},
		{"revoke a non-admin is a no-op", bert, false, nil},
		{"make bert admin", bert, true, nil},
		{"make admin twice", bert, true, nil},
		{"now anna can go", anna, false, nil},
		{"and bert is the last", bert, false, ErrLastAdmin},
		{"unknown user", bert + 100, true, ErrUserNotFound},
		{"unknown user revoke", bert + 100, false, ErrUserNotFound},
	}
	for _, s := range steps {
		if err := SetAdmin(ctx, db, s.id, s.admin); err != s.want {
			t.Fatalf("%s: err = %v, want %v", s.name, err, s.want)
		}
	}
	for id, want := range map[int64]bool{anna: false, bert: true} {
		u, err := GetUser(ctx, db, id)
		if err != nil {
			t.Fatal(err)
		}
		if u.IsAdmin != want {
			t.Errorf("%s: admin = %v, want %v", u.Username, u.IsAdmin, want)
		}
	}
}

func TestDeleteUser(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	anna := mustCreate(t, db, "anna", true)
	bert := mustCreate(t, db, "bert", false)

	steps := []struct {
		name string
		id   int64
		want error
	}{
		{"last admin", anna, ErrLastAdmin},
		{"normal user", bert, nil},
		{"already gone", bert, ErrUserNotFound},
	}
	for _, s := range steps {
		if err := DeleteUser(ctx, db, s.id); err != s.want {
			t.Fatalf("%s: err = %v, want %v", s.name, err, s.want)
		}
	}
	carla := mustCreate(t, db, "carla", true)
	if err := DeleteUser(ctx, db, anna); err != nil {
		t.Fatalf("admin with another admin left: %v", err)
	}
	users, err := ListUsers(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != carla {
		t.Errorf("users = %+v, want only carla", users)
	}
}

func TestSetPassword(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	anna := mustCreate(t, db, "anna", false)
	bert := mustCreate(t, db, "bert", false)
	now := time.Now()
	newSession := func(id int64) string {
		tok, err := NewSession(ctx, db, id, now)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	keep, other, bertTok := newSession(anna), newSession(anna), newSession(bert)

	if err := SetPassword(ctx, db, anna, "kort", ""); err != ErrPasswordTooShort {
		t.Errorf("short password: err = %v", err)
	}
	if _, _, err := LookupSession(ctx, db, other, now); err != nil {
		t.Errorf("a refused password change logged out: %v", err)
	}
	if err := SetPassword(ctx, db, anna+100, "nieuw-geheim", ""); err != ErrUserNotFound {
		t.Errorf("unknown user: err = %v", err)
	}

	if err := SetPassword(ctx, db, anna, "nieuw-geheim", keep); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LookupSession(ctx, db, keep, now); err != nil {
		t.Errorf("kept session gone: %v", err)
	}
	if _, _, err := LookupSession(ctx, db, other, now); err != ErrNoSession {
		t.Errorf("other session still works: %v", err)
	}
	if _, _, err := LookupSession(ctx, db, bertTok, now); err != nil {
		t.Errorf("another user's session gone: %v", err)
	}
	_, hash, err := FindForLogin(ctx, db, "anna")
	if err != nil {
		t.Fatal(err)
	}
	for pw, want := range map[string]bool{"nieuw-geheim": true, "geheim1": false} {
		if ok, _ := VerifyPassword(ctx, hash, pw); ok != want {
			t.Errorf("password %q verifies = %v, want %v", pw, ok, want)
		}
	}

	// Without keepToken every session goes, as for an admin reset.
	if err := SetPassword(ctx, db, anna, "nog-een-geheim", ""); err != nil {
		t.Fatal(err)
	}
	if n := sessionCount(t, db, anna); n != 0 {
		t.Errorf("sessions left after reset: %d", n)
	}
}
