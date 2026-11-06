import pytest

pytestmark = pytest.mark.api


def test_restore_finds_an_active_subscription(subscriber):
    sub = subscriber.buy_ok("premium_annual")

    resp = subscriber.api.restore(user_id=subscriber.user_id, store="app_store")

    body = resp.json()
    assert resp.status_code == 200
    assert body["restored"] is True
    assert body["restored_count"] == 1
    assert body["entitlement"]["has_access"] is True
    assert body["entitlement"]["subscriptions"][0]["original_transaction_id"] == sub["original_transaction_id"]


def test_restore_works_during_a_trial(subscriber):
    subscriber.buy_ok(trial=True)
    body = subscriber.api.restore(user_id=subscriber.user_id, store="app_store").json()
    assert body["restored"] is True
    assert body["entitlement"]["state"] == "trial"


def test_restore_with_no_purchases_finds_nothing(subscriber):
    resp = subscriber.api.restore(user_id=subscriber.user_id, store="app_store")

    assert resp.status_code == 200
    assert resp.json()["restored"] is False
    assert resp.json()["entitlement"]["state"] == "none"
    assert subscriber.events() == []


def test_restore_ignores_purchases_from_the_other_store(make_subscriber):
    user = make_subscriber(store="app_store")
    user.buy_ok()

    body = user.api.restore(user_id=user.user_id, store="play").json()
    assert body["restored"] is False


@pytest.mark.parametrize("ending", ["EXPIRED", "REFUND"])
def test_restore_does_not_bring_back_ended_subscriptions(subscriber, ending):
    sub = subscriber.buy_ok()
    subscriber.notify_ok(ending, sub["original_transaction_id"])

    body = subscriber.api.restore(user_id=subscriber.user_id, store="app_store").json()

    assert body["restored"] is False
    assert body["entitlement"]["has_access"] is False
    assert "purchase_restored" not in subscriber.event_types()


def test_restore_is_repeatable_and_creates_nothing_new(subscriber):
    subscriber.buy_ok()
    first = subscriber.api.restore(user_id=subscriber.user_id, store="app_store").json()
    second = subscriber.api.restore(user_id=subscriber.user_id, store="app_store").json()

    assert first["entitlement"] == second["entitlement"]
    assert len(subscriber.entitlement()["subscriptions"]) == 1


def test_restore_emits_an_event_with_the_count(subscriber):
    subscriber.buy_ok("premium_monthly")
    subscriber.buy_ok("premium_annual")
    subscriber.api.restore(user_id=subscriber.user_id, store="app_store")

    event = subscriber.events("purchase_restored")[0]
    assert event["event_properties"] == {"store": "app_store", "restored_count": 2}
    assert event["platform"] == "ios"


def test_restore_only_returns_the_callers_subscriptions(make_subscriber):
    owner, other = make_subscriber(), make_subscriber()
    owner.buy_ok()

    body = other.api.restore(user_id=other.user_id, store="app_store").json()
    assert body["restored"] is False


@pytest.mark.parametrize("body", [{}, {"user_id": "u1"}, {"store": "play"}, {"user_id": "u1", "store": "x"}])
def test_restore_validates_input(api, body):
    assert api.restore(**body).status_code == 400
