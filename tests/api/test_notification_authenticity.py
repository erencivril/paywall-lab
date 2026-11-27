"""Store notifications must be signed. Without this, anyone who can reach the
endpoint can grant or revoke premium by posting JSON."""
import json
import time

import pytest

from tests.support.client import sign_notification

pytestmark = pytest.mark.api


def _raw(notification_type, txn, notification_id, **extra):
    return json.dumps(
        {"notification_id": notification_id, "type": notification_type, "original_transaction_id": txn, **extra}
    ).encode()


def _assert_rejected(resp):
    assert resp.status_code == 401, resp.text
    assert resp.json()["error"]["code"] == "invalid_signature"


def test_unsigned_refund_is_rejected_and_access_stays(api, subscriber):
    sub = subscriber.buy_ok()

    resp = api.notify_raw(_raw("REFUND", sub["original_transaction_id"], "forged-" + subscriber.user_id))

    _assert_rejected(resp)
    assert subscriber.entitlement()["has_access"] is True
    assert subscriber.event_types() == ["subscription_started"]


def test_unsigned_subscribed_cannot_grant_premium(api, subscriber):
    """The spoofing case: a forged SUBSCRIBED for a transaction nobody paid for."""
    raw = _raw(
        "SUBSCRIBED", "txn-forged-" + subscriber.user_id, "forged-" + subscriber.user_id,
        user_id=subscriber.user_id, product_id="premium_annual", store="app_store", country="US",
    )

    _assert_rejected(api.notify_raw(raw))

    ent = subscriber.entitlement()
    assert ent["has_access"] is False
    assert ent["state"] == "none"
    assert subscriber.events() == []


def test_wrong_secret_is_rejected(api, subscriber):
    sub = subscriber.buy_ok()
    raw = _raw("EXPIRED", sub["original_transaction_id"], "wrongkey-" + subscriber.user_id)

    _assert_rejected(api.notify_raw(raw, sign_notification(raw, secret="not-the-secret")))

    assert subscriber.entitlement()["has_access"] is True


def test_tampered_body_is_rejected(api, subscriber):
    sub = subscriber.buy_ok()
    txn = sub["original_transaction_id"]
    harmless = _raw("DID_RENEW", txn, "tamper-" + subscriber.user_id)
    headers = sign_notification(harmless)

    # Re-use a genuine signature on a different body.
    evil = _raw("REFUND", txn, "tamper-" + subscriber.user_id)
    _assert_rejected(api.notify_raw(evil, headers))

    assert subscriber.entitlement()["has_access"] is True


def test_stale_timestamp_is_rejected_even_when_correctly_signed(api, subscriber):
    sub = subscriber.buy_ok()
    raw = _raw("EXPIRED", sub["original_transaction_id"], "stale-" + subscriber.user_id)

    _assert_rejected(api.notify_raw(raw, sign_notification(raw, timestamp=time.time() - 3600)))

    assert subscriber.entitlement()["has_access"] is True


def test_missing_timestamp_is_rejected(api, subscriber):
    sub = subscriber.buy_ok()
    raw = _raw("EXPIRED", sub["original_transaction_id"], "nots-" + subscriber.user_id)
    headers = sign_notification(raw)
    del headers["X-Paywall-Timestamp"]

    _assert_rejected(api.notify_raw(raw, headers))


def test_replaying_a_captured_request_is_deduplicated(api, subscriber):
    """Inside the tolerance window the signature is still valid. The
    notification_id check is what stops a second effect."""
    sub = subscriber.buy_ok()
    raw = _raw("DID_RENEW", sub["original_transaction_id"], "replay-" + subscriber.user_id)
    headers = sign_notification(raw)

    first = api.notify_raw(raw, headers)
    second = api.notify_raw(raw, headers)

    assert first.json()["status"] == "applied"
    assert second.status_code == 200
    assert second.json()["status"] == "duplicate"
    assert subscriber.entitlement()["subscriptions"][0]["renewal_count"] == 1


def test_a_valid_signature_does_not_bypass_validation(api):
    raw = _raw("PAUSED", "txn-x", "n-x")

    resp = api.notify_raw(raw, sign_notification(raw))

    assert resp.status_code == 400
