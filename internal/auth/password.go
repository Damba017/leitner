// Package auth holds users, sessions, password hashing and the login rate limiter.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters for new hashes. Existing hashes carry their own
// parameters, so these can change without breaking logins.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 2
	argonThreads = 2
	saltLen      = 16
	keyLen       = 32
)

const (
	MinPasswordLen = 6    // characters
	MaxPasswordLen = 1024 // bytes; argon2 accepts any length, this caps the work per request
)

var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d bytes", MaxPasswordLen)
	errBadHash          = errors.New("malformed password hash")
)

// Each hash takes 64 MiB, so at most two run at once (128 MiB on a 4 GB
// machine that also runs nestje).
var hashSlots = make(chan struct{}, 2)

// dummyHash is verified against when a login names an unknown user, so the
// response takes as long as for a real user.
var dummyHash = encodeHash(argonMemory, argonTime, argonThreads, make([]byte, saltLen), make([]byte, keyLen))

// CheckPassword reports whether pw is acceptable as a new password.
func CheckPassword(pw string) error {
	if len(pw) > MaxPasswordLen {
		return ErrPasswordTooLong
	}
	if utf8.RuneCountInString(pw) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	return nil
}

// HashPassword returns pw hashed with argon2id in PHC string format.
func HashPassword(ctx context.Context, pw string) (string, error) {
	if err := CheckPassword(pw); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	rand.Read(salt) // never fails (crypto/rand)
	key, err := idKey(ctx, pw, salt, argonTime, argonMemory, argonThreads, keyLen)
	if err != nil {
		return "", err
	}
	return encodeHash(argonMemory, argonTime, argonThreads, salt, key), nil
}

// VerifyPassword reports whether pw matches the encoded hash.
func VerifyPassword(ctx context.Context, encoded, pw string) (bool, error) {
	if len(pw) > MaxPasswordLen {
		return false, nil
	}
	m, t, p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	got, err := idKey(ctx, pw, salt, t, m, p, uint32(len(want)))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// VerifyDummy does the same work as VerifyPassword for a user that doesn't exist.
func VerifyDummy(ctx context.Context, pw string) error {
	_, err := VerifyPassword(ctx, dummyHash, pw)
	return err
}

func idKey(ctx context.Context, pw string, salt []byte, t, m uint32, p uint8, n uint32) ([]byte, error) {
	select {
	case hashSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-hashSlots }()
	return argon2.IDKey([]byte(pw), salt, t, m, p, n), nil
}

var b64 = base64.RawStdEncoding

func encodeHash(m, t uint32, p uint8, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, m, t, p, b64.EncodeToString(salt), b64.EncodeToString(key))
}

func decodeHash(s string) (m, t uint32, p uint8, salt, key []byte, err error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, errBadHash
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return 0, 0, 0, nil, nil, errBadHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return 0, 0, 0, nil, nil, errBadHash
	}
	// Bounds keep a corrupted row from allocating gigabytes or looping forever.
	if p < 1 || m < 8*uint32(p) || m > 256*1024 || t < 1 || t > 16 {
		return 0, 0, 0, nil, nil, errBadHash
	}
	if salt, err = b64.DecodeString(parts[4]); err != nil || len(salt) < 8 {
		return 0, 0, 0, nil, nil, errBadHash
	}
	if key, err = b64.DecodeString(parts[5]); err != nil || len(key) < 16 || len(key) > 64 {
		return 0, 0, 0, nil, nil, errBadHash
	}
	return m, t, p, salt, key, nil
}
