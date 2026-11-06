import pytest

from tests.e2e.pages import PaywallPage


@pytest.fixture(autouse=True)
def stack_is_up(api):
    """Make sure the app answers before a browser test starts."""


@pytest.fixture
def paywall(page):
    return PaywallPage(page)


@pytest.fixture
def console_errors(page):
    """Collects console errors and uncaught page errors during a test."""
    errors = []
    page.on("console", lambda msg: errors.append(msg.text) if msg.type == "error" else None)
    page.on("pageerror", lambda exc: errors.append(str(exc)))
    return errors
