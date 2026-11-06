package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/erencivril/paywall-lab/internal/remoteconfig"
	"github.com/erencivril/paywall-lab/internal/service"
)

// Every request here is rejected before the service touches the database, so
// the service is built without one.
func newTestHandler(t *testing.T, debug bool) http.Handler {
	t.Helper()
	cfg, err := remoteconfig.Load("../../config/remote_config.json")
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(service.New(cfg, nil), debug)
}

func do(h http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequestValidation(t *testing.T) {
	h := newTestHandler(t, false)
	const js = "application/json"
	validPurchase := `{"user_id":"u1","product_id":"premium_monthly","store":"app_store","country":"US"}`

	tests := []struct {
		name        string
		method, url string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{"paywall without country", "GET", "/v1/paywall?user_id=u1", "", "", 400, "invalid_request"},
		{"paywall with 3-letter country", "GET", "/v1/paywall?country=USA&user_id=u1", "", "", 400, "invalid_request"},
		{"paywall with numeric country", "GET", "/v1/paywall?country=12&user_id=u1", "", "", 400, "invalid_request"},
		{"paywall without user", "GET", "/v1/paywall?country=US", "", "", 400, "invalid_request"},
		{"paywall with bad user chars", "GET", "/v1/paywall?country=US&user_id=a%20b", "", "", 400, "invalid_request"},
		{"purchase wrong content type", "POST", "/v1/purchases", "text/plain", validPurchase, 415, "unsupported_media_type"},
		{"purchase no content type", "POST", "/v1/purchases", "", validPurchase, 415, "unsupported_media_type"},
		{"purchase malformed json", "POST", "/v1/purchases", js, `{"user_id":`, 400, "invalid_request"},
		{"purchase unknown field", "POST", "/v1/purchases", js, `{"user_id":"u1","price":1}`, 400, "invalid_request"},
		{"purchase trailing data", "POST", "/v1/purchases", js, validPurchase + `{}`, 400, "invalid_request"},
		{"purchase empty object", "POST", "/v1/purchases", js, `{}`, 400, "invalid_request"},
		{"purchase bad store", "POST", "/v1/purchases", js,
			`{"user_id":"u1","product_id":"premium_monthly","store":"huawei","country":"US"}`, 400, "invalid_request"},
		{"purchase missing country", "POST", "/v1/purchases", js,
			`{"user_id":"u1","product_id":"premium_monthly","store":"play"}`, 400, "invalid_request"},
		{"purchase sandbox must be bool", "POST", "/v1/purchases", js,
			`{"user_id":"u1","product_id":"premium_monthly","store":"play","country":"US","sandbox":"yes"}`, 400, "invalid_request"},
		{"restore without store", "POST", "/v1/restore", js, `{"user_id":"u1"}`, 400, "invalid_request"},
		{"notification bad type", "POST", "/v1/store-notifications", js,
			`{"notification_id":"n1","type":"PAUSED","original_transaction_id":"t1"}`, 400, "invalid_request"},
		{"notification missing id", "POST", "/v1/store-notifications", js,
			`{"type":"EXPIRED","original_transaction_id":"t1"}`, 400, "invalid_request"},
		{"notification bad expires_at", "POST", "/v1/store-notifications", js,
			`{"notification_id":"n1","type":"DID_RENEW","original_transaction_id":"t1","expires_at":"tomorrow"}`, 400, "invalid_request"},
		{"expires_at on non-renewal", "POST", "/v1/store-notifications", js,
			`{"notification_id":"n1","type":"EXPIRED","original_transaction_id":"t1","expires_at":"2030-01-01T00:00:00Z"}`, 400, "invalid_request"},
		{"entitlements bad user id", "GET", "/v1/entitlements/bad%20id", "", "", 400, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(h, tt.method, tt.url, tt.contentType, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+tt.wantCode+`"`) {
				t.Errorf("body %s should contain code %s", rec.Body, tt.wantCode)
			}
			if rec.Header().Get("X-Request-Id") == "" {
				t.Error("missing X-Request-Id header")
			}
		})
	}
}

func TestRoutingRules(t *testing.T) {
	off := newTestHandler(t, false)
	on := newTestHandler(t, true)

	if rec := do(off, "GET", "/v1/debug/events", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("debug endpoint with debug off: status = %d, want 404", rec.Code)
	}
	if rec := do(off, "GET", "/v1/purchases", "", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/purchases: status = %d, want 405", rec.Code)
	}
	if rec := do(on, "DELETE", "/v1/entitlements/u1", "", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE entitlements: status = %d, want 405", rec.Code)
	}
}

func TestPaywallPageAndAssetsAreServed(t *testing.T) {
	h := newTestHandler(t, false)
	for path, want := range map[string]string{
		"/paywall":            "<main id=\"paywall\"",
		"/assets/paywall.js":  "/v1/paywall",
		"/assets/paywall.css": ".product",
	} {
		rec := do(h, "GET", path, "", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET %s: status %d, body missing %q", path, rec.Code, want)
		}
	}
}
