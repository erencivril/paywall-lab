import pytest

pytestmark = pytest.mark.api


def test_first_renewal_converts_a_trial(subscriber):
    sub = subscriber.buy_ok(trial=True)

    result = subscriber.notify_ok("DID_RENEW", sub["original_transaction_id"])

    assert result["status"] == "applied"
    assert result["state"] == "active"
    ent = subscriber.entitlement()
    assert ent["state"] == "active"
    assert ent["has_access"] is True
    assert ent["subscriptions"][0]["renewal_count"] == 0  # converting is not a renewal


def test_renewal_extends_expiry_and_counts(subscriber):
    sub = subscriber.buy_ok("premium_monthly")

    subscriber.notify_ok("DID_RENEW", sub["original_transaction_id"])
    subscriber.notify_ok("DID_RENEW", sub["original_transaction_id"])

    renewed = subscriber.entitlement()["subscriptions"][0]
    assert renewed["renewal_count"] == 2
    assert renewed["expires_at"] > sub["expires_at"]
    assert subscriber.event_types() == ["subscription_started", "subscription_renewed", "subscription_renewed"]


def test_renewal_can_carry_its_own_expiry_date(subscriber):
    sub = subscriber.buy_ok("premium_monthly")

    subscriber.notify_ok("DID_RENEW", sub["original_transaction_id"], expires_at="2090-01-01T00:00:00Z")

    assert subscriber.entitlement()["subscriptions"][0]["expires_at"] == "2090-01-01T00:00:00Z"


def test_expired_revokes_access(subscriber):
    sub = subscriber.buy_ok(trial=True)

    subscriber.notify_ok("EXPIRED", sub["original_transaction_id"])

    ent = subscriber.entitlement()
    assert ent["has_access"] is False
    assert ent["state"] == "expired"


def test_refund_revokes_access_during_trial(subscriber):
    sub = subscriber.buy_ok(trial=True)

    subscriber.notify_ok("REFUND", sub["original_transaction_id"])

    ent = subscriber.entitlement()
    assert ent["has_access"] is False
    assert ent["state"] == "refunded"


def test_refund_revokes_access_for_an_active_subscription(subscriber):
    sub = subscriber.buy_ok("premium_annual")
    subscriber.notify_ok("DID_RENEW", sub["original_transaction_id"])

    subscriber.notify_ok("REFUND", sub["original_transaction_id"])

    assert subscriber.entitlement()["has_access"] is False


def test_refund_of_one_subscription_keeps_the_other(subscriber):
    monthly = subscriber.buy_ok("premium_monthly")
    subscriber.buy_ok("premium_annual")

    subscriber.notify_ok("REFUND", monthly["original_transaction_id"])

    ent = subscriber.entitlement()
    assert ent["has_access"] is True
    assert {s["product_id"]: s["state"] for s in ent["subscriptions"]} == {
        "premium_monthly": "refunded",
        "premium_annual": "active",
    }


def test_refund_after_expiry_is_recorded(subscriber):
    sub = subscriber.buy_ok()
    subscriber.notify_ok("EXPIRED", sub["original_transaction_id"])

    subscriber.notify_ok("REFUND", sub["original_transaction_id"])

    assert subscriber.entitlement()["state"] == "refunded"
    last = subscriber.events("subscription_refunded")[0]
    assert last["event_properties"]["previous_state"] == "expired"


def test_active_state_with_a_past_expiry_does_not_grant_access(subscriber):
    sub = subscriber.buy_ok()

    subscriber.notify_ok("DID_RENEW", sub["original_transaction_id"], expires_at="2020-01-01T00:00:00Z")

    ent = subscriber.entitlement()
    assert ent["subscriptions"][0]["state"] == "active"
    assert ent["has_access"] is False


class TestIdempotency:
    def test_same_notification_twice_is_applied_once(self, subscriber):
        sub = subscriber.buy_ok()
        txn = sub["original_transaction_id"]

        first = subscriber.notify("DID_RENEW", txn, notification_id="dup-" + subscriber.user_id)
        second = subscriber.notify("DID_RENEW", txn, notification_id="dup-" + subscriber.user_id)

        assert first.json()["status"] == "applied"
        assert second.status_code == 200
        assert second.json()["status"] == "duplicate"
        assert subscriber.entitlement()["subscriptions"][0]["renewal_count"] == 1
        assert subscriber.event_types() == ["subscription_started", "subscription_renewed"]

    def test_notification_id_is_global_not_per_transaction(self, make_subscriber):
        a, b = make_subscriber(), make_subscriber()
        sub_a, sub_b = a.buy_ok(), b.buy_ok()
        shared_id = "shared-" + a.user_id

        assert a.notify("EXPIRED", sub_a["original_transaction_id"], notification_id=shared_id).json()["status"] == "applied"
        assert b.notify("EXPIRED", sub_b["original_transaction_id"], notification_id=shared_id).json()["status"] == "duplicate"
        assert b.entitlement()["has_access"] is True

    def test_expired_twice_with_different_ids_emits_one_event(self, subscriber):
        sub = subscriber.buy_ok()
        txn = sub["original_transaction_id"]

        subscriber.notify_ok("EXPIRED", txn)
        again = subscriber.notify_ok("EXPIRED", txn)

        assert again["status"] == "no_change"
        assert subscriber.event_types().count("subscription_expired") == 1

    def test_refund_twice_emits_one_event(self, subscriber):
        sub = subscriber.buy_ok()
        txn = sub["original_transaction_id"]

        subscriber.notify_ok("REFUND", txn)
        assert subscriber.notify_ok("REFUND", txn)["status"] == "no_change"
        assert subscriber.event_types().count("subscription_refunded") == 1


class TestOutOfOrderAndUnknown:
    def test_renewal_after_refund_is_rejected(self, subscriber):
        sub = subscriber.buy_ok()
        txn = sub["original_transaction_id"]
        subscriber.notify_ok("REFUND", txn)

        resp = subscriber.notify("DID_RENEW", txn)

        assert resp.status_code == 409
        assert resp.json()["error"]["code"] == "invalid_transition"
        assert subscriber.entitlement()["state"] == "refunded"

    def test_rejected_notification_is_not_remembered(self, subscriber):
        sub = subscriber.buy_ok()
        txn = sub["original_transaction_id"]
        subscriber.notify_ok("EXPIRED", txn)
        nid = "late-" + subscriber.user_id

        first = subscriber.notify("DID_RENEW", txn, notification_id=nid)
        retry = subscriber.notify("DID_RENEW", txn, notification_id=nid)

        assert first.status_code == retry.status_code == 409

    def test_expire_after_refund_changes_nothing(self, subscriber):
        sub = subscriber.buy_ok()
        txn = sub["original_transaction_id"]
        subscriber.notify_ok("REFUND", txn)

        assert subscriber.notify_ok("EXPIRED", txn)["status"] == "no_change"
        assert subscriber.entitlement()["state"] == "refunded"

    def test_unknown_transaction_is_404_and_can_be_retried(self, subscriber):
        txn = "txn-late-" + subscriber.user_id
        nid = "n-late-" + subscriber.user_id

        before = subscriber.notify("EXPIRED", txn, notification_id=nid)
        assert before.status_code == 404
        assert before.json()["error"]["code"] == "unknown_transaction"

        subscriber.buy_ok(transaction_id=txn)  # the purchase arrives after the notification
        after = subscriber.notify("EXPIRED", txn, notification_id=nid)
        assert after.status_code == 200
        assert after.json()["status"] == "applied"


class TestSubscribedNotification:
    def test_subscribed_for_a_known_purchase_changes_nothing(self, subscriber):
        sub = subscriber.buy_ok()

        result = subscriber.notify_ok("SUBSCRIBED", sub["original_transaction_id"])

        assert result["status"] == "no_change"
        assert subscriber.event_types() == ["subscription_started"]

    def test_subscribed_can_create_a_purchase_the_client_never_reported(self, subscriber):
        txn = "txn-server-" + subscriber.user_id

        result = subscriber.notify_ok(
            "SUBSCRIBED", txn, user_id=subscriber.user_id, product_id="premium_annual",
            store="play", country="CA",
        )

        assert result["status"] == "applied"
        sub = subscriber.entitlement()["subscriptions"][0]
        assert sub["original_transaction_id"] == txn
        assert sub["tier"] == "tier1"
        assert sub["store"] == "play"

    def test_subscribed_for_an_unknown_purchase_needs_details(self, subscriber):
        resp = subscriber.notify("SUBSCRIBED", "txn-nodetails-" + subscriber.user_id)
        assert resp.status_code == 400


class TestValidation:
    @pytest.mark.parametrize(
        "change",
        [
            {"type": "PAUSED"},
            {"type": ""},
            {"notification_id": ""},
            {"original_transaction_id": "has space"},
            {"expires_at": "tomorrow"},
            {"extra": 1},
        ],
        ids=lambda c: next(iter(c)),
    )
    def test_bad_notification_is_rejected(self, api, change):
        body = {"notification_id": "n-bad", "type": "DID_RENEW", "original_transaction_id": "txn-bad"}
        resp = api.notify(**{**body, **change})
        assert resp.status_code == 400
