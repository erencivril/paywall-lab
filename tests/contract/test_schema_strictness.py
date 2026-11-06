"""Checks on the schemas themselves.

A contract test is only useful if the schema can fail. These tests take a good
event, break it in one way and expect the schema to notice. They run without
the service.
"""
import copy

import pytest
from jsonschema import Draft202012Validator

pytestmark = pytest.mark.contract

GOOD_EVENT = {
    "insert_id": "0b6b5f0e-2c0e-4c33-9d0e-1f3f4a5b6c7d",
    "event_type": "subscription_renewed",
    "user_id": "qa-user-1",
    "time": 1_760_000_000_000,
    "platform": "ios",
    "event_properties": {
        "original_transaction_id": "sim_abc",
        "product_id": "premium_monthly",
        "store": "app_store",
        "sandbox": True,
        "country": "US",
        "tier": "tier1",
        "price_cents": 999,
        "currency": "USD",
        "renewal_count": 1,
        "expires_at": 1_762_600_000_000,
    },
}


def mutate(change):
    event = copy.deepcopy(GOOD_EVENT)
    change(event)
    return event


MUTATIONS = {
    "missing_user_id": lambda e: e.pop("user_id"),
    "empty_user_id": lambda e: e.update(user_id=""),
    "missing_property": lambda e: e["event_properties"].pop("tier"),
    "extra_property": lambda e: e["event_properties"].update(coupon="X"),
    "extra_top_level_field": lambda e: e.update(session_id=1),
    "price_as_string": lambda e: e["event_properties"].update(price_cents="999"),
    "price_zero": lambda e: e["event_properties"].update(price_cents=0),
    "price_as_float": lambda e: e["event_properties"].update(price_cents=9.99),
    "sandbox_as_string": lambda e: e["event_properties"].update(sandbox="true"),
    "unknown_tier": lambda e: e["event_properties"].update(tier="tier2"),
    "lower_case_country": lambda e: e["event_properties"].update(country="us"),
    "unknown_product": lambda e: e["event_properties"].update(product_id="premium_weekly"),
    "currency_symbol": lambda e: e["event_properties"].update(currency="$"),
    "platform_not_allowed": lambda e: e.update(platform="web"),
    "time_in_seconds": lambda e: e.update(time=1_760_000_000),
    "time_as_iso_string": lambda e: e.update(time="2026-01-01T00:00:00Z"),
    "bad_insert_id": lambda e: e.update(insert_id="not-a-uuid"),
    "renewal_count_zero": lambda e: e["event_properties"].update(renewal_count=0),
    "event_type_mismatch": lambda e: e.update(event_type="subscription_expired"),
}


@pytest.fixture(scope="module")
def validator(schemas):
    return Draft202012Validator(schemas["subscription_renewed"])


def test_the_reference_event_is_valid(validator):
    assert list(validator.iter_errors(GOOD_EVENT)) == []


@pytest.mark.parametrize("name", MUTATIONS)
def test_schema_rejects_broken_event(validator, name):
    broken = mutate(MUTATIONS[name])
    assert list(validator.iter_errors(broken)), f"schema accepted a broken event ({name})"


def test_every_event_type_has_its_own_const(schemas):
    for name, schema in schemas.items():
        assert schema["properties"]["event_type"] == {"const": name}


def test_plan_documents_every_event_and_property(plan):
    for name, spec in plan["events"].items():
        assert spec.get("description"), f"{name} has no description"
        assert spec.get("trigger"), f"{name} has no trigger"
        assert spec["platforms"] and set(spec["platforms"]) <= {"ios", "android", "web"}, name
        for prop, prop_spec in spec["properties"].items():
            assert prop_spec.get("description"), f"{name}.{prop} has no description"
            assert prop_spec.get("type") in {"string", "integer", "boolean"}, f"{name}.{prop} has no valid type"
