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
