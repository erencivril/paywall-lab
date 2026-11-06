import pytest

pytestmark = pytest.mark.api


def test_unknown_user_has_no_access(api, user_id):
    resp = api.entitlements(user_id)

    assert resp.status_code == 200
    assert resp.json() == {"user_id": user_id, "has_access": False, "state": "none", "subscriptions": []}


def test_entitlement_follows_the_whole_lifecycle(subscriber):
    states = []
    sub = subscriber.buy_ok(trial=True)
    states.append(subscriber.entitlement()["state"])
    subscriber.notify_ok("DID_RENEW", sub["original_transaction_id"])
    states.append(subscriber.entitlement()["state"])
    subscriber.notify_ok("EXPIRED", sub["original_transaction_id"])
    states.append(subscriber.entitlement()["state"])

    assert states == ["trial", "active", "expired"]


def test_users_do_not_see_each_others_subscriptions(make_subscriber):
    a, b = make_subscriber(), make_subscriber()
    a.buy_ok()

    assert b.entitlement()["has_access"] is False
    assert a.entitlement()["has_access"] is True


def test_access_comes_from_any_subscription_that_still_grants_it(subscriber):
    old = subscriber.buy_ok("premium_monthly")
    subscriber.buy_ok("premium_annual")
    subscriber.notify_ok("EXPIRED", old["original_transaction_id"])

    ent = subscriber.entitlement()
    assert ent["has_access"] is True
    assert ent["state"] == "active"


@pytest.mark.parametrize("bad", ["a b", "x" * 129, "semi;colon"])
def test_bad_user_id_is_rejected(api, bad):
    assert api.entitlements(bad).status_code == 400
