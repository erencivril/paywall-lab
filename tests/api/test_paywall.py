import pytest

from tests.support import price_book as pb

pytestmark = pytest.mark.api


@pytest.mark.parametrize("country,tier", sorted(pb.COUNTRY_TIER.items()))
def test_prices_and_trial_follow_the_country_tier(api, user_id, country, tier):
    resp = api.paywall(country, user_id)

    assert resp.status_code == 200
    body = resp.json()
    assert body["tier"] == tier
    assert body["country"] == country
    assert body["trial_days"] == pb.TRIAL_DAYS[tier]
    prices = {p["product_id"]: p for p in body["products"]}
    assert set(prices) == set(pb.PRODUCTS)
    for product_id, cents in pb.PRICES[tier].items():
        assert prices[product_id]["price_cents"] == cents
        assert prices[product_id]["currency"] == pb.CURRENCY
        assert prices[product_id]["display_price"] == pb.display_price(cents)


def test_country_code_is_case_insensitive(api, user_id):
    lower = api.paywall("us", user_id).json()
    upper = api.paywall("US", user_id).json()
    assert lower == upper
    assert lower["country"] == "US"


def test_unlisted_country_gets_the_default_tier(api, user_id):
    body = api.paywall("ZZ", user_id).json()
    assert body["tier"] == "ex_tier1"
    assert body["products"][0]["price_cents"] == pb.PRICES["ex_tier1"]["premium_monthly"]


@pytest.mark.parametrize(
    "country,user",
    [
        (None, "u1"),
        ("", "u1"),
        ("USA", "u1"),
        ("U", "u1"),
        ("1X", "u1"),
        ("US", None),
        ("US", ""),
        ("US", "has space"),
        ("US", "x" * 129),
        ("US", "semi;colon"),
    ],
)
def test_bad_input_is_rejected(api, country, user):
    resp = api.paywall(country, user)
    assert resp.status_code == 400
    assert resp.json()["error"]["code"] == "invalid_request"


def test_same_user_always_gets_the_same_variant(api, user_id):
    variants = {api.paywall("US", user_id).json()["variant"] for _ in range(5)}
    assert len(variants) == 1


def test_variant_does_not_depend_on_country(api, user_id):
    variants = {api.paywall(country, user_id).json()["variant"] for country in ("US", "BR", "JP", "IN")}
    assert len(variants) == 1


@pytest.mark.parametrize("user,variant", sorted(pb.PINNED_VARIANTS.items()))
def test_known_users_keep_their_variant(api, user, variant):
    assert api.paywall("US", user).json()["variant"] == variant


def test_both_variants_are_served(api):
    seen = {api.paywall("US", f"split-check-{i}").json()["variant"] for i in range(40)}
    assert seen == {"control", "annual_first"}


@pytest.mark.parametrize("user,variant", sorted(pb.PINNED_VARIANTS.items()))
def test_variant_changes_copy_and_highlight_but_not_prices(api, user, variant):
    body = api.paywall("US", user).json()
    highlighted = [p["product_id"] for p in body["products"] if p["highlighted"]]
    expected = {"control": "premium_monthly", "annual_first": "premium_annual"}[variant]
    assert highlighted == [expected]
    assert {p["product_id"]: p["price_cents"] for p in body["products"]} == pb.PRICES["tier1"]


def test_cta_text_mentions_the_trial_length_of_the_tier(api):
    for country, days in (("US", 7), ("BR", 3)):
        body = api.paywall(country, "golden-2").json()
        assert str(days) in body["cta_text"], body["cta_text"]


def test_viewing_the_paywall_emits_one_event(api, user_id):
    body = api.paywall("DE", user_id).json()
    events = api.events(user_id)
    assert [e["event_type"] for e in events] == ["paywall_viewed"]
    assert events[0]["event_properties"] == {
        "country": "DE",
        "tier": "tier1",
        "variant": body["variant"],
        "config_version": body["config_version"],
    }


def test_bad_request_emits_no_event(api, user_id):
    api.paywall("USA", user_id)
    assert api.events(user_id) == []
