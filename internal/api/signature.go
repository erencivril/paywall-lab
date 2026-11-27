package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Header names used by the store (or the proxy in front of it) to sign a
// notification. The signed payload is "<timestamp>.<raw body>", the same
// scheme Stripe uses for webhooks. Signing the timestamp together with the
// body means an attacker cannot replay a captured request outside the
// tolerance window by changing only the timestamp header.
const (
	HeaderSignature = "X-Paywall-Signature" // "v1=<hex hmac-sha256>"
	HeaderTimestamp = "X-Paywall-Timestamp" // unix seconds
	signaturePrefix = "v1="

	// DefaultTolerance is how far the timestamp may be from the server clock.
	DefaultTolerance = 5 * time.Minute

	maxNotificationBody = 1 << 20
)

var (
	errMissingSignature = errors.New("missing signature header")
	errMissingTimestamp = errors.New("missing timestamp header")
	errBadTimestamp     = errors.New("timestamp is not a unix time in seconds")
	errStaleTimestamp   = errors.New("timestamp outside the allowed window")
	errBadSignature     = errors.New("signature does not match")
)

// SignatureVerifier checks the HMAC-SHA256 signature on store notifications.
// Real stores protect their webhooks the same way in spirit: App Store Server
// Notifications V2 are JWS documents signed with Apple's certificate chain,
// and Google Play RTDN arrives through Pub/Sub push with an OIDC token that
// must be verified. This project has no store keys, so it uses a shared
// secret instead.
type SignatureVerifier struct {
	secret    []byte
	tolerance time.Duration
	now       func() time.Time
}

// NewSignatureVerifier returns a verifier. An empty secret is a programming
// error: callers must refuse to start without one. A non-positive tolerance
// uses DefaultTolerance.
func NewSignatureVerifier(secret string, tolerance time.Duration) (*SignatureVerifier, error) {
	if secret == "" {
		return nil, errors.New("notification signing secret is empty")
	}
	if tolerance <= 0 {
		tolerance = DefaultTolerance
	}
	return &SignatureVerifier{secret: []byte(secret), tolerance: tolerance, now: time.Now}, nil
}

// Sign returns the header value for a body signed at the given time. It is
// exported so tests and tools can produce valid requests.
func Sign(secret string, at time.Time, body []byte) (timestamp, signature string) {
	timestamp = strconv.FormatInt(at.Unix(), 10)
	return timestamp, signaturePrefix + hex.EncodeToString(mac([]byte(secret), timestamp, body))
}

func mac(secret []byte, timestamp string, body []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(timestamp))
	h.Write([]byte("."))
	h.Write(body)
	return h.Sum(nil)
}

// Verify checks headers against the raw body.
func (v *SignatureVerifier) Verify(h http.Header, body []byte) error {
	sigHeader := strings.TrimSpace(h.Get(HeaderSignature))
	if sigHeader == "" {
		return errMissingSignature
	}
	tsHeader := strings.TrimSpace(h.Get(HeaderTimestamp))
	if tsHeader == "" {
		return errMissingTimestamp
	}
	secs, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return errBadTimestamp
	}
	skew := v.now().Sub(time.Unix(secs, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > v.tolerance {
		return errStaleTimestamp
	}
	got, err := hex.DecodeString(strings.TrimPrefix(sigHeader, signaturePrefix))
	if err != nil || !strings.HasPrefix(sigHeader, signaturePrefix) {
		return errBadSignature
	}
	// hmac.Equal compares in constant time.
	if !hmac.Equal(got, mac(v.secret, tsHeader, body)) {
		return errBadSignature
	}
	return nil
}

// requireSignature rejects a request with 401 unless its signature is valid.
// It runs before any parsing, on the raw bytes, and then hands the same bytes
// to the next handler. A nil verifier rejects everything (fail closed).
func requireSignature(v *SignatureVerifier, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if v == nil {
			writeError(w, http.StatusUnauthorized, "invalid_signature", "notification signature is required")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxNotificationBody))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "could not read request body")
			return
		}
		if err := v.Verify(r.Header, body); err != nil {
			// The reason is logged, not returned, so a caller learns nothing
			// about which check failed.
			slog.Warn("notification rejected", "reason", err.Error(), "request_id", logSafe(w.Header().Get("X-Request-Id")))
			writeError(w, http.StatusUnauthorized, "invalid_signature", "notification signature is missing or invalid")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next(w, r)
	}
}
