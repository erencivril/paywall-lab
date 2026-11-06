import pytest

from tests.support import price_book as pb
from tests.support.timeutil import days, length

pytestmark = pytest.mark.api


class TestHappyPaths:
    def test_trial_purchase_in_a_tier1_country(self, subscriber):
        sub = subscriber.buy_ok("premium_monthly", trial=True)

        assert sub["state"] == "trial"
        assert sub["tier"] == "tier1"
        assert sub["price_cents"] == 999
        assert sub["currency"] == "USD"
        assert sub["has_access"] is True
        assert sub["sandbox"] is True
        assert length(sub) == days(7)
        assert sub["original_transaction_id"].startswith("sim_")

    def test_trial_is_shorter_in_ex_tier1(self, make_subscriber):
        sub = make_subscriber(country="BR").buy_ok("premium_monthly", trial=True)

        assert sub["tier"] == "ex_tier1"
        assert sub["price_cents"] == 499
        assert length(sub) == days(3)

    def test_purchase_without_trial_starts_active(self, subscriber):
        sub = subscriber.buy_ok("premium_annual")

        assert sub["state"] == "active"
        assert sub["price_cents"] == 5999
        assert length(sub) == days(365)
        assert sub["renewal_count"] == 0

    @pytest.mark.parametrize("country,tier", [("US", "tier1"), ("GB", "tier1"), ("IN", "ex_tier1"), ("TR", "ex_tier1")])
    @pytest.mark.parametrize("product_id", pb.PRODUCTS)
    def test_charged_price_matches_the_price_book(self, make_subscriber, country, tier, product_id):
        sub = make_subscriber(country=country).buy_ok(product_id)
        assert sub["price_cents"] == pb.PRICES[tier][product_id]

    def test_play_store_purchase(self, make_subscriber):
        sub = make_subscriber(store="play").buy_ok("premium_monthly")
        assert sub["store"] == "play"

    def test_production_purchase_is_not_marked_sandbox(self, subscriber):
        sub = subscriber.buy_ok("premium_monthly", sandbox=False)
        assert sub["sandbox"] is False

    def test_lower_case_country_is_accepted(self, subscriber):
        sub = subscriber.buy_ok("premium_monthly", country="us")
        assert sub["tier"] == "tier1"

    def test_purchase_grants_entitlement(self, subscriber):
        subscriber.buy_ok("premium_monthly", trial=True)

        ent = subscriber.entitlement()
        assert ent["has_access"] is True
        assert ent["state"] == "trial"
        assert len(ent["subscriptions"]) == 1


class TestInvalidInput:
    BASE = {"user_id": "u-invalid", "product_id": "premium_monthly", "store": "app_store", "country": "US"}

    @pytest.mark.parametrize(
        "change",
        [
            {"user_id": ""},
            {"user_id": "bad id"},
            {"product_id": ""},
            {"store": "huawei"},
            {"store": ""},
            {"country": ""},
            {"country": "USA"},
            {"sandbox": "yes"},
            {"start_trial": "true"},
            {"transaction_id": "not valid!"},
            {"price_cents": 1},  # the client must not be able to set a price
        ],
        ids=lambda c: next(iter(c)) + "=" + repr(next(iter(c.values()))),
    )
    def test_bad_field_is_rejected(self, api, change):
        resp = api.purchase(**{**self.BASE, **change})
        assert resp.status_code == 400, resp.text
        assert resp.json()["error"]["code"] == "invalid_request"

    @pytest.mark.parametrize("missing", ["user_id", "product_id", "store", "country"])
    def test_missing_required_field_is_rejected(self, api, missing):
        body = {k: v for k, v in self.BASE.items() if k != missing}
        resp = api.purchase(**body)
        assert resp.status_code == 400
        assert missing in resp.json()["error"]["message"]

    def test_unknown_product_is_rejected(self, subscriber):
        resp = subscriber.buy("premium_weekly")
        assert resp.status_code == 422
        assert resp.json()["error"]["code"] == "unknown_product"

    def test_non_json_body_is_rejected(self, api):
        resp = api.request("POST", "/v1/purchases", data="user_id=u1", headers={"Content-Type": "text/plain"})
        assert resp.status_code == 415

    def test_broken_json_is_rejected(self, api):
        resp = api.request("POST", "/v1/purchases", data="{", headers={"Content-Type": "application/json"})
        assert resp.status_code == 400

    def test_rejected_purchase_changes_nothing(self, subscriber):
        subscriber.buy("premium_weekly")
        subscriber.buy("premium_monthly", store="huawei")

        assert subscriber.entitlement()["state"] == "none"
        assert subscriber.events() == []


class TestRules:
    def test_cannot_buy_the_same_product_twice(self, subscriber):
        subscriber.buy_ok("premium_monthly")

        resp = subscriber.buy("premium_monthly")
        assert resp.status_code == 409
        assert resp.json()["error"]["code"] == "already_subscribed"
        assert len(subscriber.entitlement()["subscriptions"]) == 1

    def test_free_trial_can_only_be_used_once(self, subscriber):
        subscriber.buy_ok("premium_monthly", trial=True)

        resp = subscriber.buy("premium_annual", trial=True)
        assert resp.status_code == 409
        assert resp.json()["error"]["code"] == "trial_already_used"

    def test_a_refused_second_trial_does_not_block_a_normal_purchase(self, subscriber):
        subscriber.buy_ok("premium_monthly", trial=True)
        subscriber.buy("premium_annual", trial=True)

        sub = subscriber.buy_ok("premium_annual", trial=False)
        assert sub["state"] == "active"

    def test_same_product_can_be_bought_again_after_expiry(self, subscriber):
        first = subscriber.buy_ok("premium_monthly")
        subscriber.notify_ok("EXPIRED", first["original_transaction_id"])

        second = subscriber.buy_ok("premium_monthly")
        assert second["original_transaction_id"] != first["original_transaction_id"]
        assert subscriber.entitlement()["has_access"] is True

    def test_two_products_at_once_are_allowed(self, subscriber):
        subscriber.buy_ok("premium_monthly")
        subscriber.buy_ok("premium_annual")
        assert len(subscriber.entitlement()["subscriptions"]) == 2


class TestIdempotency:
    def test_retry_with_the_same_transaction_id_returns_the_original(self, subscriber):
        first = subscriber.buy("premium_monthly", transaction_id="txn-" + subscriber.user_id)
        retry = subscriber.buy("premium_monthly", transaction_id="txn-" + subscriber.user_id)

        assert first.status_code == 201
        assert first.json()["duplicate"] is False
        assert retry.status_code == 200
        assert retry.json()["duplicate"] is True
        assert retry.json()["subscription"] == first.json()["subscription"]

    def test_retry_does_not_emit_a_second_event(self, subscriber):
        txn = "txn-" + subscriber.user_id
        subscriber.buy("premium_monthly", transaction_id=txn)
        subscriber.buy("premium_monthly", transaction_id=txn)

        assert subscriber.event_types() == ["subscription_started"]

    def test_transaction_id_cannot_be_claimed_by_another_user(self, subscriber, make_subscriber):
        txn = "txn-" + subscriber.user_id
        subscriber.buy_ok("premium_monthly", transaction_id=txn)

        resp = make_subscriber().buy("premium_monthly", transaction_id=txn)
        assert resp.status_code == 409
        assert resp.json()["error"]["code"] == "transaction_conflict"

    def test_transaction_id_cannot_be_reused_for_another_product(self, subscriber):
        txn = "txn-" + subscriber.user_id
        subscriber.buy_ok("premium_monthly", transaction_id=txn)

        resp = subscriber.buy("premium_annual", transaction_id=txn)
        assert resp.status_code == 409
