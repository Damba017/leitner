package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

// The example from RFC 8291 section 5 and appendix A, copied verbatim
// (whitespace is removed before decoding).
const (
	rfcPlaintext  = "V2hlbiBJIGdyb3cgdXAsIEkgd2FudCB0byBiZSBhIHdhdGVybWVsb24"
	rfcASPublic   = "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIg Dll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"
	rfcASPrivate  = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	rfcUAPublic   = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcx aOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfcUAPrivate  = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	rfcSalt       = "DGv6ra1nlYgDCS1FRnbzlw"
	rfcAuthSecret = "BTBZMqHH6r4Tts7J_aSIgg"
	rfcECDHSecret = "kyrL1jIIOHEzg3sM2ZWRHDRB62YACZhhSlknJ672kSs"
	rfcKeyInfo    = `V2ViUHVzaDogaW5mbwAEJXGyvs3942BVGq8e0PTNNmwR zr5VX4m8t7GGpTM5FzFo7OLr4BhZe9MEebhuPI-OztV3
		ylkYfpJGmQ22ggCLDgT-M_SrDepxkU21WCP3O1SUj0Ew bZIHMtu5pZpTKGSCIA5Zent7wmC6HCJ5mFgJkuk5cwAv MBKiiujwa7t45ewP`
	rfcCEK    = "oIhVW04MRdy2XN9CiKLxTg"
	rfcNonce  = "4h_95klXJ5E_qnoN"
	rfcHeader = `DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z 9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml
		mlMoZIIgDll6e3vCYLocInmYWAmS6Tlz AC8wEqKK6PBru3jl7A8`
	rfcCiphertext = `8pfeW0KbunFT06SuDKoJH9Ql87S1QUrd irN6GcG7sFz1y1sqLgVi1VhjVkHsUoEs
		bI_0LpXMuGvnzQ`
	rfcBody = `DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml
		mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT
		pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN`
)

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return b
}

func TestEncryptRFC8291Example(t *testing.T) {
	plaintext := unb64(t, rfcPlaintext)
	if string(plaintext) != "When I grow up, I want to be a watermelon" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	asKey, err := ecdh.P256().NewPrivateKey(unb64(t, rfcASPrivate))
	if err != nil {
		t.Fatal(err)
	}
	asPublic := asKey.PublicKey().Bytes()
	if !bytes.Equal(asPublic, unb64(t, rfcASPublic)) {
		t.Fatal("application server public key doesn't match its private key")
	}
	uaPublic := unb64(t, rfcUAPublic)
	authSecret := unb64(t, rfcAuthSecret)
	salt := unb64(t, rfcSalt)

	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		t.Fatal(err)
	}
	ecdhSecret, err := asKey.ECDH(ua)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, got, want []byte) {
		t.Helper()
		if !bytes.Equal(got, want) {
			t.Errorf("%s =\n%s\nwant\n%s", name, b64.EncodeToString(got), b64.EncodeToString(want))
		}
	}
	check("ecdh_secret", ecdhSecret, unb64(t, rfcECDHSecret))
	check("key_info", []byte(keyInfo(uaPublic, asPublic)), unb64(t, rfcKeyInfo))

	cek, nonce, err := contentKeys(ecdhSecret, authSecret, salt, uaPublic, asPublic)
	if err != nil {
		t.Fatal(err)
	}
	check("CEK", cek, unb64(t, rfcCEK))
	check("NONCE", nonce, unb64(t, rfcNonce))

	body, err := encrypt(plaintext, uaPublic, authSecret, asKey, salt)
	if err != nil {
		t.Fatal(err)
	}
	check("header", body[:headerLen], unb64(t, rfcHeader))
	check("ciphertext", body[headerLen:], unb64(t, rfcCiphertext))
	check("body", body, unb64(t, rfcBody))

	// The receiving side: the user agent's private key opens the record.
	uaKey, err := ecdh.P256().NewPrivateKey(unb64(t, rfcUAPrivate))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(uaKey.PublicKey().Bytes(), uaPublic) {
		t.Fatal("user agent public key doesn't match its private key")
	}
	check("decrypted", decrypt(t, unb64(t, rfcBody), uaKey, authSecret), plaintext)
}

func TestEncryptRoundTrip(t *testing.T) {
	uaKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authSecret := bytes.Repeat([]byte{7}, authLen)
	salt := bytes.Repeat([]byte{9}, saltLen)
	asKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1, 100, MaxPayload} {
		plaintext := bytes.Repeat([]byte("x"), size)
		body, err := encrypt(plaintext, uaKey.PublicKey().Bytes(), authSecret, asKey, salt)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if len(body) > recordSize {
			t.Errorf("size %d: body is %d bytes, over %d", size, len(body), recordSize)
		}
		if got := decrypt(t, body, uaKey, authSecret); !bytes.Equal(got, plaintext) {
			t.Errorf("size %d: round trip gave %d bytes", size, len(got))
		}
	}
	_, err = encrypt(make([]byte, MaxPayload+1), uaKey.PublicKey().Bytes(), authSecret, asKey, salt)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("too large: err = %v, want ErrPayloadTooLarge", err)
	}
	if _, err := encrypt(nil, []byte("not a key"), authSecret, asKey, salt); err == nil {
		t.Error("bad user agent key: no error")
	}
}

// decrypt plays the user agent: it reads the header, derives the same keys
// and opens the single record.
func decrypt(t *testing.T, body []byte, uaKey *ecdh.PrivateKey, authSecret []byte) []byte {
	t.Helper()
	salt, asPublic := body[:saltLen], body[saltLen+5:headerLen]
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	ecdhSecret, err := uaKey.ECDH(as)
	if err != nil {
		t.Fatal(err)
	}
	cek, nonce, err := contentKeys(ecdhSecret, authSecret, salt, uaKey.PublicKey().Bytes(), asPublic)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	record, err := gcm.Open(nil, nonce, body[headerLen:], nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(record) == 0 || record[len(record)-1] != 2 {
		t.Fatal("record doesn't end in the 0x02 delimiter")
	}
	return record[:len(record)-1]
}
