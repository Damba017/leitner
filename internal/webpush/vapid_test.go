package webpush

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestAudience(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string // "" = error
	}{
		{"https://fcm.googleapis.com/fcm/send/abc", "https://fcm.googleapis.com"},
		{"https://web.push.apple.com/QGx?x=1", "https://web.push.apple.com"},
		{"https://push.example.net:8443/push/x", "https://push.example.net:8443"},
		{"http://127.0.0.1:9000/push", "http://127.0.0.1:9000"},
		{"ftp://push.example.net/x", ""},
		{"/push/x", ""},
		{"https:///x", ""},
	}
	for _, tt := range tests {
		got, err := audience(tt.endpoint)
		if tt.want == "" {
			if err == nil {
				t.Errorf("audience(%q) = %q, want error", tt.endpoint, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("audience(%q) = %q, %v; want %q", tt.endpoint, got, err, tt.want)
		}
	}
}

func TestVAPIDHeader(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Unix(1_800_000_000, 0)
	h, err := vapidHeader(key, pub, "https://updates.push.services.mozilla.com/wpush/v2/abc", "https://leitner.example.com", exp)
	if err != nil {
		t.Fatal(err)
	}

	token, k, ok := strings.Cut(strings.TrimPrefix(h, "vapid t="), ", k=")
	if !strings.HasPrefix(h, "vapid t=") || !ok {
		t.Fatalf("header = %q, want \"vapid t=<jwt>, k=<key>\"", h)
	}
	if k != b64.EncodeToString(pub) {
		t.Errorf("k = %q, want the public key", k)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts, want 3", len(parts))
	}
	var header map[string]string
	if err := json.Unmarshal(unb64(t, parts[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header["typ"] != "JWT" || header["alg"] != "ES256" || len(header) != 2 {
		t.Errorf("JWT header = %v", header)
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(unb64(t, parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != "https://updates.push.services.mozilla.com" || claims.Exp != exp.Unix() || claims.Sub != "https://leitner.example.com" {
		t.Errorf("claims = %+v", claims)
	}

	sig := unb64(t, parts[2])
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want 64 (r||s)", len(sig))
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		t.Error("signature doesn't verify with the public key")
	}
}
