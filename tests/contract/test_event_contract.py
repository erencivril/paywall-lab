"""Every event the service emits must match the tracking plan."""
import pytest
from jsonschema import Draft202012Validator

from tests.contract.scenarios import SCENARIOS
from tests.support import price_book as pb

pytestmark = pytest.mark.contract

CLOCK_SKEW_MS = 5_000


def validation_errors(schema, event):
    validator = Draft202012Validator(schema)
    return [f"{'/'.join(map(str, e.absolute_path)) or '<event>'}: {e.message}" for e in validator.iter_errors(event)]


@pytest.mark.parametrize("scenario", SCENARIOS)
def test_events_match_their_schema(journeys, schemas, scenario):
    events = journeys["scenarios"][scenario]["events"]
    assert events, "scenario produced no events"
    for event in events:
        assert event["event_type"] in schemas, f"{event['event_type']} is not in tracking-plan.yaml"
        assert validation_errors(schemas[event["event_type"]], event) == []


@pytest.mark.parametrize("scenario", SCENARIOS)
def test_events_come_in_the_expected_order(journeys, scenario):
    run = journeys["scenarios"][scenario]
    assert [e["event_type"] for e in run["events"]] == run["expected"]


def test_every_event_in_the_plan_is_emitted_by_some_scenario(plan, all_events):
    emitted = {e["event_type"] for e in all_events}
    assert set(plan["events"]) - emitted == set(), "plan events no scenario triggers"


def test_no_event_outside_the_plan_is_emitted(plan, all_events):
    emitted = {e["event_type"] for e in all_events}
    assert emitted - set(plan["events"]) == set()


def test_insert_ids_are_unique(all_events):
    ids = [e["insert_id"] for e in all_events]
    assert len(ids) == len(set(ids))


def test_event_time_is_in_milliseconds_and_current(journeys, all_events):
    started, finished = journeys["window"]
    for event in all_events:
        assert started - CLOCK_SKEW_MS <= event["time"] <= finished + CLOCK_SKEW_MS, event


def test_subscription_events_carry_the_price_of_the_users_tier(all_events):
    checked = 0
    for event in all_events:
        props = event["event_properties"]
        if "price_cents" not in props:
            continue
        checked += 1
        assert props["tier"] == pb.COUNTRY_TIER[props["country"]], event
        assert props["price_cents"] == pb.PRICES[props["tier"]][props["product_id"]], event
        assert props["currency"] == pb.CURRENCY
    assert checked > 0


def test_platform_matches_the_store(all_events):
    expected = {"app_store": "ios", "play": "android"}
    for event in all_events:
        store = event["event_properties"].get("store")
        if store:
            assert event["platform"] == expected[store], event


def test_trial_started_reports_the_trial_length_of_the_tier(all_events):
    trials = [e for e in all_events if e["event_type"] == "trial_started"]
    assert trials
    for event in trials:
        assert event["event_properties"]["trial_days"] == pb.TRIAL_DAYS[event["event_properties"]["tier"]]


def test_duplicate_notifications_do_not_duplicate_events(journeys):
    events = journeys["scenarios"]["duplicates_are_not_double_counted"]["events"]
    assert [e["event_type"] for e in events].count("subscription_renewed") == 1


def test_sandbox_flag_is_reported(all_events):
    flagged = [e for e in all_events if "sandbox" in e["event_properties"]]
    assert flagged and all(e["event_properties"]["sandbox"] is True for e in flagged)
