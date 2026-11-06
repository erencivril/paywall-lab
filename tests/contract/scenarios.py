"""User journeys that together cause every event in the tracking plan.

Each scenario takes a Subscriber, drives the API and returns the event types
it expects to have been emitted, in order.
"""


def paywall_in_tier1_country(user):
    assert user.api.paywall("US", user.user_id).status_code == 200
    return ["paywall_viewed"]


def paywall_in_ex_tier1_country(user):
    assert user.api.paywall("BR", user.user_id).status_code == 200
    return ["paywall_viewed"]


def trial_converts_then_renews(user):
    user.api.paywall(user.country, user.user_id)
    sub = user.buy_ok("premium_monthly", trial=True)
    user.notify_ok("DID_RENEW", sub["original_transaction_id"])
    user.notify_ok("DID_RENEW", sub["original_transaction_id"])
    return ["paywall_viewed", "trial_started", "trial_converted", "subscription_renewed"]


def android_purchase_expires(user):
    user.store, user.country = "play", "BR"
    sub = user.buy_ok("premium_annual")
    user.notify_ok("EXPIRED", sub["original_transaction_id"])
    return ["subscription_started", "subscription_expired"]


def trial_refunded(user):
    sub = user.buy_ok("premium_monthly", trial=True)
    user.notify_ok("REFUND", sub["original_transaction_id"])
    return ["trial_started", "subscription_refunded"]


def expired_then_refunded(user):
    user.country = "JP"
    sub = user.buy_ok("premium_annual")
    user.notify_ok("EXPIRED", sub["original_transaction_id"])
    user.notify_ok("REFUND", sub["original_transaction_id"])
    return ["subscription_started", "subscription_expired", "subscription_refunded"]


def restore_after_purchase(user):
    user.country = "IN"
    user.buy_ok("premium_monthly")
    assert user.api.restore(user_id=user.user_id, store=user.store).json()["restored"] is True
    return ["subscription_started", "purchase_restored"]


def duplicates_are_not_double_counted(user):
    sub = user.buy_ok("premium_monthly")
    for _ in range(3):
        user.notify("DID_RENEW", sub["original_transaction_id"], notification_id="same-" + user.user_id)
    return ["subscription_started", "subscription_renewed"]


SCENARIOS = {
    fn.__name__: fn
    for fn in (
        paywall_in_tier1_country,
        paywall_in_ex_tier1_country,
        trial_converts_then_renews,
        android_purchase_expires,
        trial_refunded,
        expired_then_refunded,
        restore_after_purchase,
        duplicates_are_not_double_counted,
    )
}
