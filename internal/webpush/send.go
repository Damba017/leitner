package webpush

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	sendTimeout = 10 * time.Second
	// RFC 8292 and the push services allow at most 24 hours.
	jwtLifetime  = 12 * time.Hour
	maxErrorBody = 512
)

var (
	// ErrGone means the push service no longer knows the subscription (the
	// user revoked it or it expired); it should be deleted.
	ErrGone = errors.New("subscription expired or revoked")
	// ErrNoSubject means there is no contact URL (the -url flag), which the
	// push services require.
	ErrNoSubject = errors.New("no VAPID subject")
)

// Options are delivery settings for the push service.
type Options struct {
	TTL     time.Duration // how long an undelivered message is kept
	Urgency string        // "very-low", "low", "normal" or "high"; empty for the default
}

// Sender signs and sends push messages with the server's VAPID key.
type Sender struct {
	key     *ecdsa.PrivateKey
	pub     []byte
	subject string
	client  *http.Client
}

// NewSender returns a sender for key. subject is the contact URL the push
// services get (the -url flag); without one, Send returns ErrNoSubject.
func NewSender(key *ecdsa.PrivateKey, subject string) (*Sender, error) {
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	return &Sender{key: key, pub: pub, subject: subject, client: &http.Client{
		Timeout: sendTimeout,
		// Push services answer directly; a redirect would send the request
		// somewhere CheckEndpoint never approved.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// PublicKey is the applicationServerKey browsers subscribe with.
func (s *Sender) PublicKey() []byte { return s.pub }

// CanSend reports whether the sender has a subject to send with.
func (s *Sender) CanSend() bool { return s.subject != "" }

// Send encrypts payload for sub and posts it to the push service. It returns
// ErrGone when the subscription no longer exists.
func (s *Sender) Send(ctx context.Context, sub Subscription, payload []byte, opts Options) error {
	if s.subject == "" {
		return ErrNoSubject
	}
	asKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, saltLen)
	rand.Read(salt) // never fails
	body, err := encrypt(payload, sub.P256dh, sub.Auth, asKey, salt)
	if err != nil {
		return err
	}
	auth, err := vapidHeader(s.key, s.pub, sub.Endpoint, s.subject, time.Now().Add(jwtLifetime))
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return errBadEndpoint
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(opts.TTL/time.Second)))
	if opts.Urgency != "" {
		req.Header.Set("Urgency", opts.Urgency)
	}
	res, err := s.client.Do(req)
	if err != nil {
		// url.Error repeats the whole endpoint, which works like a password.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return fmt.Errorf("webpush: send: %w", err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return nil
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return ErrGone
	}
	// Apple, for one, says why in the body, e.g. {"reason":"BadJwtToken"}.
	msg, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	return fmt.Errorf("webpush: push service answered %s: %s", res.Status, bytes.TrimSpace(msg))
}
