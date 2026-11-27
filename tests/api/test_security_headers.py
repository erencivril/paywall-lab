import pytest

pytestmark = pytest.mark.api


@pytest.mark.parametrize("path", ["/paywall", "/assets/paywall.js", "/healthz"])
def test_responses_carry_browser_hardening_headers(api, path):
    resp = api.request("GET", path)

    assert resp.status_code == 200
    assert resp.headers["X-Content-Type-Options"] == "nosniff"
    assert resp.headers["X-Frame-Options"] == "DENY"
    assert resp.headers["Referrer-Policy"] == "no-referrer"
    assert "default-src 'self'" in resp.headers["Content-Security-Policy"]


@pytest.mark.parametrize("query", ["user_id=%00", "user_id=a%20b", "user_id=ok&event_type=%00"])
def test_debug_events_rejects_malformed_filters(api, query):
    """ZAP found user_id=%00 returning 500 because the value reached PostgreSQL."""
    resp = api.request("GET", f"/v1/debug/events?{query}")

    assert resp.status_code == 400
    assert resp.json()["error"]["code"] == "invalid_request"
