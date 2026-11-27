package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersOnPageAndAPI(t *testing.T) {
	h := newTestHandler(t, false)
	for _, path := range []string{"/paywall", "/assets/paywall.js", "/healthz", "/v1/paywall"} {
		rec := do(h, "GET", path, "", "")
		for name, want := range map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"X-Frame-Options":              "DENY",
			"Referrer-Policy":              "no-referrer",
			"Cross-Origin-Resource-Policy": "same-origin",
		} {
			if got := rec.Header().Get(name); got != want {
				t.Errorf("GET %s: %s = %q, want %q", path, name, got, want)
			}
		}
		if csp := rec.Header().Get("Content-Security-Policy"); csp == "" {
			t.Errorf("GET %s: missing Content-Security-Policy", path)
		}
	}
}

func TestRequestIDFromClientIsSanitised(t *testing.T) {
	h := newTestHandler(t, false)
	for name, tc := range map[string]struct {
		in      string
		keepsIt bool
	}{
		"plain id is echoed":     {"trace-123.abc", true},
		"too long is replaced":   {strings.Repeat("a", 65), false},
		"space is replaced":      {"a b", false},
		"semicolon is replaced":  {"a;b", false},
		"empty gets a fresh one": {"", false},
		"unicode is replaced":    {"idé", false},
		"64 characters are fine": {strings.Repeat("a", 64), true},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/healthz", nil)
			if tc.in != "" {
				req.Header.Set("X-Request-Id", tc.in)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			got := rec.Header().Get("X-Request-Id")
			if tc.keepsIt && got != tc.in {
				t.Errorf("X-Request-Id = %q, want %q", got, tc.in)
			}
			if !tc.keepsIt && (got == tc.in || !requestIDPattern.MatchString(got)) {
				t.Errorf("X-Request-Id = %q, want a generated id", got)
			}
		})
	}
}

func TestLogSafeStripsLineBreaks(t *testing.T) {
	if got := logSafe("a\r\nb\nc"); got != "abc" {
		t.Errorf("logSafe = %q, want %q", got, "abc")
	}
	if got := logSafe(strings.Repeat("x", 1000)); len(got) != 256 {
		t.Errorf("logSafe length = %d, want 256", len(got))
	}
}

func TestDebugEventsRejectsBadFilters(t *testing.T) {
	h := newTestHandler(t, true)
	for _, target := range []string{
		"/v1/debug/events?user_id=%00",
		"/v1/debug/events?user_id=a%20b",
		"/v1/debug/events?user_id=ok&event_type=%00",
	} {
		if rec := do(h, "GET", target, "", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", target, rec.Code)
		}
	}
}
