import pytest

pytestmark = pytest.mark.api


def test_health_reports_ok(api):
    resp = api.health()
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


def test_every_response_has_a_request_id(api):
    resp = api.health()
    assert len(resp.headers["X-Request-Id"]) >= 8


def test_request_id_from_the_caller_is_kept(api):
    resp = api.request("GET", "/healthz", headers={"X-Request-Id": "trace-123"})
    assert resp.headers["X-Request-Id"] == "trace-123"


def test_debug_events_endpoint_is_enabled_in_the_test_stack(api, user_id):
    assert api.request("GET", "/v1/debug/events", params={"user_id": user_id}).status_code == 200
