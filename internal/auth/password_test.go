package auth

import (
	"context"
	"strings"
	"testing"
)

func TestCheckPassword(t *testing.T) {
	tests := []struct {
		pw   string
		want error
	}{
		{"", ErrPasswordTooShort},
		{"12345", ErrPasswordTooShort},
		{"123456", nil},
		{"ééééé", ErrPasswordTooShort}, // 5 characters, 10 bytes: counted in characters
		{"éééééé", nil},
		{strings.Repeat("a", MaxPasswordLen), nil},
		{strings.Repeat("a", MaxPasswordLen+1), ErrPasswordTooLong},
	}
	for _, tt := range tests {
		if got := CheckPassword(tt.pw); got != tt.want {
			t.Errorf("CheckPassword(%.20q) = %v, want %v", tt.pw, got, tt.want)
		}
	}
}

func TestHashAndVerify(t *testing.T) {
	ctx := context.Background()
	hash, err := HashPassword(ctx, "geheim1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=2,p=2$") {
		t.Errorf("hash = %q", hash)
	}
	hash2, err := HashPassword(ctx, "geheim1")
	if err != nil {
		t.Fatal(err)
	}
	if hash == hash2 {
		t.Error("two hashes of the same password are equal: no random salt")
	}

	tests := []struct {
		pw   string
		want bool
	}{
		{"geheim1", true},
		{"geheim2", false},
		{"Geheim1", false},
		{"geheim1 ", false},
		{"", false},
		{strings.Repeat("a", MaxPasswordLen+1), false},
	}
	for _, tt := range tests {
		got, err := VerifyPassword(ctx, hash, tt.pw)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("VerifyPassword(%.20q) = %v, want %v", tt.pw, got, tt.want)
		}
	}

	if _, err := HashPassword(ctx, "kort"); err != ErrPasswordTooShort {
		t.Errorf("HashPassword(short) err = %v", err)
	}
	if err := VerifyDummy(ctx, "anything"); err != nil {
		t.Errorf("VerifyDummy: %v", err)
	}
}

func TestVerifyMalformedHash(t *testing.T) {
	ctx := context.Background()
	good, err := HashPassword(ctx, "geheim1")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(good, "$") // "", argon2id, v=19, params, salt, key
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v
		return strings.Join(p, "$")
	}
	for _, h := range []string{
		"",
		"plain",
		"$argon2i$v=19$m=65536,t=2,p=2$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5",
		with(1, "argon2i"),
		with(2, "v=16"),
		with(3, "m=65536,t=2"),
		with(3, "m=1048576,t=2,p=2"), // 1 GiB
		with(3, "m=65536,t=0,p=2"),
		with(3, "m=65536,t=100,p=2"),
		with(3, "m=65536,t=2,p=0"),
		with(3, "m=65536,t=2,p=300"),
		with(4, "!!"),
		with(4, "c2FsdA"), // 4-byte salt
		with(5, "a2V5"),   // 3-byte key
		good + "$extra",
	} {
		if ok, err := VerifyPassword(ctx, h, "geheim1"); ok || err == nil {
			t.Errorf("VerifyPassword(%q) = %v, %v; want false, error", h, ok, err)
		}
	}
}

func TestHashCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Fill the slots so idKey has to wait, then it must give up on ctx.
	for range cap(hashSlots) {
		hashSlots <- struct{}{}
	}
	defer func() {
		for range cap(hashSlots) {
			<-hashSlots
		}
	}()
	if _, err := HashPassword(ctx, "geheim1"); err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
