package webpush

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newSender(t *testing.T, subject string) *Sender {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSender(key, subject)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// newSub plays a browser subscribing at endpoint; the key opens the messages.
func newSub(t *testing.T, endpoint string) (Subscription, *ecdh.PrivateKey) {
	t.Helper()
	uaKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, authLen)
	rand.Read(auth)
	return Subscription{Endpoint: endpoint, P256dh: uaKey.PublicKey().Bytes(), Auth: auth}, uaKey
}

type pushed struct {
	method, path string
	header       http.Header
	body         []byte
}

func TestSend(t *testing.T) {
	got := make(chan pushed, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- pushed{r.Method, r.URL.Path, r.Header.Clone(), body}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	s := newSender(t, "https://leitner.example.com")
	sub, uaKey := newSub(t, srv.URL+"/push/secret")
	payload := []byte(`{"title":"Leitner","body":"Dit is een testmelding."}`)
	start := time.Now()
	if err := s.Send(t.Context(), sub, payload, Options{TTL: 5 * time.Minute, Urgency: "high"}); err != nil {
		t.Fatal(err)
	}
	p := <-got

	if p.method != http.MethodPost || p.path != "/push/secret" {
		t.Errorf("request = %s %s", p.method, p.path)
	}
	for name, want := range map[string]string{
		"Content-Encoding": "aes128gcm",
		"Content-Type":     "application/octet-stream",
		"TTL":              "300",
		"Urgency":          "high",
	} {
		if v := p.header.Get(name); v != want {
			t.Errorf("%s = %q, want %q", name, v, want)
		}
	}
	if plain := decrypt(t, p.body, uaKey, sub.Auth); !bytes.Equal(plain, payload) {
		t.Errorf("decrypted = %q, want %q", plain, payload)
	}

	token, k, _ := strings.Cut(strings.TrimPrefix(p.header.Get("Authorization"), "vapid t="), ", k=")
	if k != b64.EncodeToString(s.PublicKey()) {
		t.Errorf("k = %q, want the sender's public key", k)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT %q has %d parts", token, len(parts))
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(unb64(t, parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != srv.URL || claims.Sub != "https://leitner.example.com" {
		t.Errorf("claims = %+v, want aud %q", claims, srv.URL)
	}
	if exp := time.Unix(claims.Exp, 0); exp.Before(start.Add(time.Hour)) || exp.After(start.Add(24*time.Hour)) {
		t.Errorf("exp = %v, want between 1 and 24 hours after %v", exp, start)
	}
}

func TestSendStatus(t *testing.T) {
	redirected := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/created":
			w.WriteHeader(http.StatusCreated)
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/gone":
			w.WriteHeader(http.StatusGone)
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/broken":
			http.Error(w, `{"reason":"BadJwtToken"}`, http.StatusForbidden)
		case "/redirect":
			http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
		case "/target":
			redirected <- struct{}{}
		}
	}))
	defer srv.Close()

	s := newSender(t, "https://leitner.example.com")
	tests := []struct {
		path    string
		wantErr error  // checked with errors.Is
		wantMsg string // "" = no error expected
	}{
		{"/created", nil, ""},
		{"/ok", nil, ""},
		{"/gone", ErrGone, ""},
		{"/notfound", ErrGone, ""},
		{"/broken", nil, "403 Forbidden: {\"reason\":\"BadJwtToken\"}"},
		{"/redirect", nil, "307"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			sub, _ := newSub(t, srv.URL+tt.path)
			err := s.Send(t.Context(), sub, []byte("hoi"), Options{TTL: time.Minute})
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantMsg != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("err = %v, want it to contain %q", err, tt.wantMsg)
				}
			case err != nil:
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
	select {
	case <-redirected:
		t.Error("the redirect was followed")
	default:
	}
}

func TestSendErrors(t *testing.T) {
	sub, _ := newSub(t, "https://fcm.googleapis.com/fcm/send/secret")
	if err := newSender(t, "").Send(t.Context(), sub, []byte("hoi"), Options{}); !errors.Is(err, ErrNoSubject) {
		t.Errorf("without subject: err = %v, want ErrNoSubject", err)
	}

	s := newSender(t, "https://leitner.example.com")
	if err := s.Send(t.Context(), sub, make([]byte, MaxPayload+1), Options{}); !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("large payload: err = %v, want ErrPayloadTooLarge", err)
	}

	// A connection error must not repeat the endpoint in the log.
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	sub, _ = newSub(t, srv.URL+"/push/secret")
	err := s.Send(t.Context(), sub, []byte("hoi"), Options{})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("closed server: err = %v, want an error without the endpoint", err)
	}
}

func TestCheckEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     error
	}{
		{"https://web.push.apple.com/QGx_abc", nil},
		{"https://fcm.googleapis.com/fcm/send/abc:def", nil},
		{"https://updates.push.services.mozilla.com/wpush/v2/abc", nil},
		{"https://wns2-par02p.notify.windows.com/w/?token=abc", nil},
		{"HTTPS://FCM.GOOGLEAPIS.COM/fcm/send/abc", nil},
		{"http://fcm.googleapis.com/fcm/send/abc", ErrInvalidEndpoint},
		{"https://user@fcm.googleapis.com/fcm/send/abc", ErrInvalidEndpoint},
		{"https:///fcm/send/abc", ErrInvalidEndpoint},
		{"", ErrInvalidEndpoint},
		{"https://fcm.googleapis.com/" + strings.Repeat("a", maxEndpointLen), ErrInvalidEndpoint},
		{"https://fcm.googleapis.com:8443/fcm/send/abc", ErrUnknownService},
		{"https://fcm.googleapis.com.evil.example/x", ErrUnknownService},
		{"https://evil.example/fcm.googleapis.com", ErrUnknownService},
		{"https://notpush.apple.com/x", ErrUnknownService},
		{"https://127.0.0.1/x", ErrUnknownService},
		{"https://localhost/x", ErrUnknownService},
	}
	for _, tt := range tests {
		if err := CheckEndpoint(tt.endpoint); err != tt.want {
			t.Errorf("CheckEndpoint(%.60q) = %v, want %v", tt.endpoint, err, tt.want)
		}
	}
}

func TestInputParse(t *testing.T) {
	uaKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p256dh := b64.EncodeToString(uaKey.PublicKey().Bytes())
	auth := b64.EncodeToString(bytes.Repeat([]byte{1}, authLen))
	endpoint := "https://fcm.googleapis.com/fcm/send/abc"
	offCurve := b64.EncodeToString(append([]byte{4}, make([]byte, 64)...))

	tests := []struct {
		name string
		in   Input
		want error
	}{
		{"valid", Input{endpoint, p256dh, auth, "  Safari · Mac  "}, nil},
		{"padded base64", Input{endpoint, p256dh + "=", auth + "==", ""}, nil},
		{"unknown service", Input{"https://evil.example/x", p256dh, auth, ""}, ErrUnknownService},
		{"not base64", Input{endpoint, "!!", auth, ""}, ErrInvalidKeys},
		{"short key", Input{endpoint, p256dh[:40], auth, ""}, ErrInvalidKeys},
		{"key off the curve", Input{endpoint, offCurve, auth, ""}, ErrInvalidKeys},
		{"short auth", Input{endpoint, p256dh, b64.EncodeToString(make([]byte, 15)), ""}, ErrInvalidKeys},
		{"long label", Input{endpoint, p256dh, auth, strings.Repeat("é", maxLabelLen+1)}, ErrInvalidLabel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, err := tt.in.Parse()
			if err != tt.want {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if err != nil {
				return
			}
			if sub.Endpoint != endpoint || !bytes.Equal(sub.P256dh, uaKey.PublicKey().Bytes()) || len(sub.Auth) != authLen {
				t.Errorf("sub = %+v", sub)
			}
			if sub.Label != strings.TrimSpace(tt.in.Label) {
				t.Errorf("label = %q", sub.Label)
			}
		})
	}
}
