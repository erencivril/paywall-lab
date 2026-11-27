package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/erencivril/paywall-lab/internal/remoteconfig"
	"github.com/erencivril/paywall-lab/internal/service"
)

// A body that passes JSON decoding but fails validation (bad type). If the
// signature check lets a request through, the handler answers 400
// invalid_request. If it rejects the request, the answer is 401. Either way
// the service never touches the database, so no store is needed.
const notifyBody = `{"notification_id":"n1","type":"PAUSED","original_transaction_id":"t1"}`

var fixedNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func signedRequest(secret string, at time.Time, body string) *http.Request {
	req := httptest.NewRequest("POST", "/v1/store-notifications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ts, sig := Sign(secret, at, []byte(body))
	req.Header.Set(HeaderTimestamp, ts)
	req.Header.Set(HeaderSignature, sig)
	return req
}

func handlerAt(t *testing.T, verifier *SignatureVerifier) http.Handler {
	t.Helper()
	cfg, err := remoteconfig.Load("../../config/remote_config.json")
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(service.New(cfg, nil), false, verifier)
}

func fixedVerifier(t *testing.T) *SignatureVerifier {
	t.Helper()
	v, err := NewSignatureVerifier(testSecret, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	v.now = func() time.Time { return fixedNow }
	return v
}

func TestSignatureVerification(t *testing.T) {
	good := func() *http.Request { return signedRequest(testSecret, fixedNow, notifyBody) }

	tests := []struct {
		name       string
		build      func() *http.Request
		wantStatus int
	}{
		{"valid signature reaches validation", good, http.StatusBadRequest},
		{"valid signature at the edge of the window", func() *http.Request {
			return signedRequest(testSecret, fixedNow.Add(-5*time.Minute), notifyBody)
		}, http.StatusBadRequest},
		{"missing signature header", func() *http.Request {
			r := good()
			r.Header.Del(HeaderSignature)
			return r
		}, http.StatusUnauthorized},
		{"missing timestamp header", func() *http.Request {
			r := good()
			r.Header.Del(HeaderTimestamp)
			return r
		}, http.StatusUnauthorized},
		{"no headers at all", func() *http.Request {
			r := httptest.NewRequest("POST", "/v1/store-notifications", strings.NewReader(notifyBody))
			r.Header.Set("Content-Type", "application/json")
			return r
		}, http.StatusUnauthorized},
		{"tampered body", func() *http.Request {
			r := signedRequest(testSecret, fixedNow, notifyBody)
			ts, sig := r.Header.Get(HeaderTimestamp), r.Header.Get(HeaderSignature)
			tampered := strings.Replace(notifyBody, `"PAUSED"`, `"REFUND"`, 1)
			r2 := httptest.NewRequest("POST", "/v1/store-notifications", strings.NewReader(tampered))
			r2.Header.Set("Content-Type", "application/json")
			r2.Header.Set(HeaderTimestamp, ts)
			r2.Header.Set(HeaderSignature, sig)
			return r2
		}, http.StatusUnauthorized},
		{"wrong secret", func() *http.Request {
			return signedRequest("some-other-secret", fixedNow, notifyBody)
		}, http.StatusUnauthorized},
		{"stale timestamp (signed correctly, 6 minutes old)", func() *http.Request {
			return signedRequest(testSecret, fixedNow.Add(-6*time.Minute), notifyBody)
		}, http.StatusUnauthorized},
		{"timestamp too far in the future", func() *http.Request {
			return signedRequest(testSecret, fixedNow.Add(6*time.Minute), notifyBody)
		}, http.StatusUnauthorized},
		{"fresh timestamp header on an old signature", func() *http.Request {
			// Replay: capture an old signed request, change only the timestamp.
			r := signedRequest(testSecret, fixedNow.Add(-time.Hour), notifyBody)
			r.Header.Set(HeaderTimestamp, strconv.FormatInt(fixedNow.Unix(), 10))
			return r
		}, http.StatusUnauthorized},
		{"timestamp is not a number", func() *http.Request {
			r := good()
			r.Header.Set(HeaderTimestamp, "yesterday")
			return r
		}, http.StatusUnauthorized},
		{"signature is not hex", func() *http.Request {
			r := good()
			r.Header.Set(HeaderSignature, "v1=not-hex")
			return r
		}, http.StatusUnauthorized},
		{"signature without the v1 prefix", func() *http.Request {
			r := good()
			r.Header.Set(HeaderSignature, strings.TrimPrefix(r.Header.Get(HeaderSignature), "v1="))
			return r
		}, http.StatusUnauthorized},
		{"empty signature value", func() *http.Request {
			r := good()
			r.Header.Set(HeaderSignature, "v1=")
			return r
		}, http.StatusUnauthorized},
	}
	h := handlerAt(t, fixedVerifier(t))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tt.build())
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus == http.StatusUnauthorized && !strings.Contains(rec.Body.String(), `"code":"invalid_signature"`) {
				t.Errorf("body %s should carry code invalid_signature", rec.Body)
			}
		})
	}
}

func TestUnsignedNotificationIsRejectedWithoutAVerifier(t *testing.T) {
	// Fail closed: if the server was wired without a verifier, a correctly
	// formed request is still refused.
	h := handlerAt(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(testSecret, time.Now(), notifyBody))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSignatureErrorDoesNotRevealWhichCheckFailed(t *testing.T) {
	h := handlerAt(t, fixedVerifier(t))
	bodies := map[string]bool{}
	for _, build := range []func() *http.Request{
		func() *http.Request { return signedRequest("wrong", fixedNow, notifyBody) },
		func() *http.Request { return signedRequest(testSecret, fixedNow.Add(-time.Hour), notifyBody) },
		func() *http.Request {
			r := signedRequest(testSecret, fixedNow, notifyBody)
			r.Header.Del(HeaderSignature)
			return r
		},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, build())
		bodies[rec.Body.String()] = true
	}
	if len(bodies) != 1 {
		t.Errorf("401 responses differ by failure reason: %v", bodies)
	}
}

func TestOtherEndpointsDoNotNeedASignature(t *testing.T) {
	h := handlerAt(t, fixedVerifier(t))
	rec := do(h, "GET", "/v1/paywall?user_id=u1", "", "")
	if rec.Code != http.StatusBadRequest { // reaches validation, not 401
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestNewSignatureVerifierRejectsEmptySecret(t *testing.T) {
	if _, err := NewSignatureVerifier("", time.Minute); err == nil {
		t.Fatal("empty secret must be refused")
	}
}

func TestSignIsStable(t *testing.T) {
	// Pins the wire format so a refactor cannot silently change it:
	// HMAC-SHA256(secret, "<unix>.<body>"), hex, prefixed with "v1=". The
	// expected value was computed with Python's hmac module, not with Sign.
	ts, sig := Sign("k", time.Unix(1700000000, 0), []byte(`{"a":1}`))
	if ts != "1700000000" {
		t.Fatalf("timestamp = %q", ts)
	}
	const want = "v1=1b6ad1bc9bf48c52ce01939866d5877c6cc4e87ddad27e16673cb8c4964394a3"
	if sig != want {
		t.Fatalf("signature = %s, want %s", sig, want)
	}
}
