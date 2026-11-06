import uuid

import pytest

from tests.support.client import ApiClient
from tests.support.flows import Subscriber
from tests.support.wait import wait_for


@pytest.fixture(scope="session")
def api(base_url):
    """API client for the running stack. Waits until /healthz answers."""
    client = ApiClient(base_url)
    wait_for(lambda: client.health().status_code == 200, timeout=60, message=f"{base_url}/healthz")
    return client


@pytest.fixture
def user_id():
    """A user id nobody else has used, so tests never share state."""
    return f"qa-{uuid.uuid4().hex[:12]}"


@pytest.fixture
def subscriber(api, user_id):
    return Subscriber(api, user_id)


@pytest.fixture
def make_subscriber(api):
    """Factory for tests that need several users or non-default countries."""

    def _make(**kwargs):
        return Subscriber(api, **kwargs)

    return _make
