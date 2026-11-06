import re

import pytest
from playwright.sync_api import expect

from tests.support import price_book as pb

pytestmark = pytest.mark.e2e

TIER1_SAMPLE = ["US", "GB", "JP"]
EX_TIER1_SAMPLE = ["BR", "IN", "TR"]


@pytest.mark.parametrize(
    "country,tier",
    [(c, "tier1") for c in TIER1_SAMPLE] + [(c, "ex_tier1") for c in EX_TIER1_SAMPLE],
)
def test_user_sees_the_price_and_trial_of_their_tier(paywall, user_id, country, tier):
    paywall.open(country, user_id).wait_until_ready()

    expect(paywall.root).to_have_attribute("data-tier", tier)
    for product_id, cents in pb.PRICES[tier].items():
        expect(paywall.price(product_id)).to_contain_text(pb.display_price(cents))
    expect(paywall.trial_text).to_have_text(f"{pb.TRIAL_DAYS[tier]}-day free trial. Cancel anytime.")
    expect(paywall.cta).to_contain_text(str(pb.TRIAL_DAYS[tier]))


def test_tier1_and_ex_tier1_users_see_different_monthly_prices(page, paywall, user_id):
    paywall.open("US", user_id).wait_until_ready()
    tier1_price = paywall.price("premium_monthly").inner_text()

    paywall.open("BR", user_id).wait_until_ready()
    ex_tier1_price = paywall.price("premium_monthly").inner_text()

    assert tier1_price.startswith("$9.99")
    assert ex_tier1_price.startswith("$4.99")


def test_control_variant_copy_and_highlight(paywall):
    paywall.open("US", "golden-2").wait_until_ready()  # pinned to control

    expect(paywall.root).to_have_attribute("data-variant", "control")
    expect(paywall.title).to_have_text("Go Premium")
    expect(paywall.cta).to_have_text("Start 7-day free trial")
    expect(paywall.product("premium_monthly")).to_have_attribute("aria-checked", "true")
    expect(paywall.product("premium_annual")).to_have_attribute("aria-checked", "false")
    expect(paywall.badge("premium_monthly")).to_be_visible()
    expect(paywall.badge("premium_annual")).to_have_count(0)


def test_annual_first_variant_copy_and_highlight(paywall):
    paywall.open("US", "golden-1").wait_until_ready()  # pinned to annual_first

    expect(paywall.root).to_have_attribute("data-variant", "annual_first")
    expect(paywall.title).to_have_text("Save with the yearly plan")
    expect(paywall.cta).to_have_text("Try 7 days free")
    expect(paywall.product("premium_annual")).to_have_attribute("aria-checked", "true")
    expect(paywall.badge("premium_annual")).to_be_visible()
    expect(paywall.badge("premium_monthly")).to_have_count(0)


def test_same_user_sees_the_same_variant_after_reload(page, paywall, user_id):
    paywall.open("US", user_id).wait_until_ready()
    first = paywall.root.get_attribute("data-variant")

    page.reload()
    paywall.wait_until_ready()

    expect(paywall.root).to_have_attribute("data-variant", first)


def test_user_can_pick_another_plan(paywall, user_id):
    paywall.open("US", "golden-2").wait_until_ready()

    paywall.product("premium_annual").click()

    expect(paywall.product("premium_annual")).to_have_attribute("aria-checked", "true")
    expect(paywall.product("premium_monthly")).to_have_attribute("aria-checked", "false")


def test_page_loads_without_console_errors(paywall, user_id, console_errors):
    paywall.open("DE", user_id).wait_until_ready()
    assert console_errors == []


def test_page_load_records_one_paywall_viewed_event(paywall, api, user_id):
    paywall.open("CA", user_id).wait_until_ready()

    events = api.events(user_id)
    assert [e["event_type"] for e in events] == ["paywall_viewed"]
    assert events[0]["event_properties"]["tier"] == "tier1"


class TestPurchaseFromThePage:
    def test_starting_a_trial_grants_access_and_emits_events(self, paywall, api, user_id):
        paywall.open("US", user_id).wait_until_ready()

        paywall.cta.click()

        expect(paywall.status).to_have_text("Subscribed (trial)")
        expect(paywall.cta).to_be_disabled()  # no double submit
        entitlement = api.entitlements(user_id).json()
        assert entitlement["has_access"] is True
        assert entitlement["subscriptions"][0]["price_cents"] == pb.PRICES["tier1"][entitlement["subscriptions"][0]["product_id"]]
        assert [e["event_type"] for e in api.events(user_id)] == ["paywall_viewed", "trial_started"]

    def test_purchase_in_ex_tier1_is_charged_the_lower_price(self, paywall, api, user_id):
        paywall.open("IN", user_id).wait_until_ready()
        paywall.product("premium_annual").click()

        paywall.cta.click()

        expect(paywall.status).to_have_text("Subscribed (trial)")
        sub = api.entitlements(user_id).json()["subscriptions"][0]
        assert (sub["product_id"], sub["price_cents"], sub["tier"]) == ("premium_annual", 2999, "ex_tier1")

    def test_second_trial_shows_the_server_error(self, paywall, subscriber):
        subscriber.buy_ok("premium_monthly", trial=True)
        paywall.open("US", subscriber.user_id).wait_until_ready()
        paywall.product("premium_annual").click()

        paywall.cta.click()

        expect(paywall.status).to_contain_text("free trial already used")
        expect(paywall.status).to_have_attribute("data-result", "trial_already_used")
        expect(paywall.cta).to_be_enabled()  # the user can try again

    def test_restore_with_nothing_to_restore(self, paywall, user_id):
        paywall.open("US", user_id).wait_until_ready()

        paywall.restore.click()

        expect(paywall.status).to_have_text("No purchases found")

    def test_restore_finds_an_earlier_purchase(self, paywall, subscriber):
        subscriber.buy_ok("premium_annual")
        paywall.open("US", subscriber.user_id).wait_until_ready()

        paywall.restore.click()

        expect(paywall.status).to_have_text("Purchases restored")

    def test_restore_after_refund_finds_nothing(self, paywall, subscriber):
        sub = subscriber.buy_ok("premium_annual")
        subscriber.notify_ok("REFUND", sub["original_transaction_id"])
        paywall.open("US", subscriber.user_id).wait_until_ready()

        paywall.restore.click()

        expect(paywall.status).to_have_text("No purchases found")


class TestErrors:
    def test_invalid_country_shows_an_error_and_no_prices(self, paywall, user_id):
        paywall.open("USA", user_id)

        expect(paywall.error).to_contain_text("two-letter")
        expect(paywall.error).to_have_attribute("data-error-code", "invalid_request")
        expect(paywall.cta).to_have_count(0)

    def test_missing_country_shows_an_error(self, page, paywall):
        page.goto("/paywall?user_id=someone")
        expect(paywall.error).to_contain_text("country is required")

    def test_server_failure_shows_a_generic_error(self, page, paywall, user_id):
        page.route("**/v1/paywall*", lambda route: route.fulfill(status=500, body="boom"))

        paywall.open("US", user_id)

        expect(paywall.error).to_have_text("Could not load the paywall.")
        expect(paywall.price("premium_monthly")).to_have_count(0)

    def test_network_failure_shows_a_generic_error(self, page, paywall, user_id):
        page.route("**/v1/paywall*", lambda route: route.abort())

        paywall.open("US", user_id)

        expect(paywall.error).to_have_attribute("data-error-code", "network")


def test_paywall_fits_a_phone_screen(browser, playwright, base_url, user_id):
    context = browser.new_context(**playwright.devices["iPhone 13"], base_url=base_url)
    page = context.new_page()
    try:
        page.goto(f"/paywall?country=US&user_id={user_id}")
        expect(page.locator("#paywall")).to_have_attribute("data-state", "ready")
        expect(page.get_by_test_id("cta")).to_be_in_viewport()
        expect(page.get_by_test_id("price-premium_monthly")).to_have_text(re.compile(r"\$9\.99"))
    finally:
        context.close()
