package webpush

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"time"
)

var b64 = base64.RawURLEncoding

// The JWT header is the same for every token.
var jwtHeader = b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))

var errBadEndpoint = errors.New("webpush: invalid endpoint")

// GenerateKey makes a new VAPID key pair.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// vapidHeader returns the Authorization value for a push to endpoint
// (RFC 8292): a JWT signed with the server's key, and that key's public half.
func vapidHeader(key *ecdsa.PrivateKey, pub []byte, endpoint, subject string, exp time.Time) (string, error) {
	aud, err := audience(endpoint)
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{aud, exp.Unix(), subject})
	if err != nil {
		return "", err
	}
	unsigned := jwtHeader + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	// ES256 wants r and s as two fixed 32-byte halves, not ASN.1.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + unsigned + "." + b64.EncodeToString(sig) + ", k=" + b64.EncodeToString(pub), nil
}

// audience is the origin of the push service: scheme and host, with the port
// if the endpoint has one.
func audience(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errBadEndpoint
	}
	return u.Scheme + "://" + u.Host, nil
}
