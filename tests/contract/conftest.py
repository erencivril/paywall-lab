import time

import pytest
from jsonschema import Draft202012Validator

from tests.contract.scenarios import SCENARIOS
from tests.support.tracking_plan import build_schemas, load_plan


@pytest.fixture(scope="session")
def plan():
    return load_plan()


@pytest.fixture(scope="session")
def schemas():
    built = build_schemas()
    for schema in built.values():
        Draft202012Validator.check_schema(schema)  # a broken schema would pass everything
    return built


@pytest.fixture(scope="session")
def journeys(api):
    """Run every scenario once and keep the events each one produced."""
    from tests.support.flows import Subscriber

    results = {}
    started_ms = int(time.time() * 1000)
    for name, scenario in SCENARIOS.items():
        user = Subscriber(api)
        expected = scenario(user)
        results[name] = {"user": user, "expected": expected, "events": user.events()}
    finished_ms = int(time.time() * 1000)
    return {"scenarios": results, "window": (started_ms, finished_ms)}


@pytest.fixture(scope="session")
def all_events(journeys):
    return [e for run in journeys["scenarios"].values() for e in run["events"]]
