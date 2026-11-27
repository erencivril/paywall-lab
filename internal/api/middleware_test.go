package api

import "testing"

func TestSecurityHeadersOnPageAndAPI(t *testing.T) {
	h := newTestHandler(t, false)
	for _, path := range []string{"/paywall", "/assets/paywall.js", "/healthz", "/v1/paywall"} {
		rec := do(h, "GET", path, "", "")
		for name, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "no-referrer",
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
