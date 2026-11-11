// Package webpush sends Web Push messages: payload encryption (RFC 8291),
// VAPID signing (RFC 8292) and the users' stored subscriptions.
package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	recordSize = 4096
	keyLen     = 65 // uncompressed P-256 point
	authLen    = 16
	saltLen    = 16
	headerLen  = saltLen + 4 + 1 + keyLen
	tagLen     = 16

	// MaxPayload is the largest plaintext that keeps the body within the 4096
	// octets every push service must accept (RFC 8291 section 4).
	MaxPayload = recordSize - headerLen - 1 - tagLen
)

var ErrPayloadTooLarge = errors.New("push payload too large")

// encrypt seals plaintext for one user agent with the aes128gcm content
// coding, as a single record (RFC 8291 section 3.4). asKey and salt must be
// new for every message; they are parameters so the RFC example can be checked.
func encrypt(plaintext, uaPublic, authSecret []byte, asKey *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > MaxPayload {
		return nil, ErrPayloadTooLarge
	}
	if len(authSecret) != authLen || len(salt) != saltLen {
		return nil, errors.New("webpush: auth secret and salt must be 16 bytes")
	}
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("webpush: user agent key: %w", err)
	}
	ecdhSecret, err := asKey.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := asKey.PublicKey().Bytes()
	cek, nonce, err := contentKeys(ecdhSecret, authSecret, salt, uaPublic, asPublic)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	out := make([]byte, headerLen, headerLen+len(plaintext)+1+tagLen)
	copy(out, salt)
	binary.BigEndian.PutUint32(out[saltLen:], recordSize)
	out[saltLen+4] = keyLen
	copy(out[saltLen+5:], asPublic)
	// 0x02 marks the last (here: only) record; no further padding.
	record := append(append(make([]byte, 0, len(plaintext)+1), plaintext...), 2)
	return gcm.Seal(out, nonce, record, nil), nil
}

// contentKeys mixes the ECDH and authentication secrets into the content
// encryption key and nonce. The nonce needs no XOR with a sequence number:
// the only record is number zero.
func contentKeys(ecdhSecret, authSecret, salt, uaPublic, asPublic []byte) (cek, nonce []byte, err error) {
	prkKey, err := hkdf.Extract(sha256.New, ecdhSecret, authSecret)
	if err != nil {
		return nil, nil, err
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo(uaPublic, asPublic), 32)
	if err != nil {
		return nil, nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, nil, err
	}
	if cek, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16); err != nil {
		return nil, nil, err
	}
	if nonce, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12); err != nil {
		return nil, nil, err
	}
	return cek, nonce, nil
}

func keyInfo(uaPublic, asPublic []byte) string {
	return "WebPush: info\x00" + string(uaPublic) + string(asPublic)
}
