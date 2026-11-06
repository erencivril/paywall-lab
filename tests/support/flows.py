"""Helpers that drive common sequences, so tests read as steps."""
import itertools
import uuid

_notification_counter = itertools.count(1)


class Subscriber:
    """One test user with a few shortcuts for the calls tests repeat."""

    def __init__(self, api, user_id=None, country="US", store="app_store"):
        self.api = api
        self.user_id = user_id or f"qa-{uuid.uuid4().hex[:12]}"
        self.country = country
        self.store = store

    def buy(self, product_id="premium_monthly", trial=False, **overrides):
        body = {
            "user_id": self.user_id,
            "product_id": product_id,
            "store": self.store,
            "country": self.country,
            "sandbox": True,
            "start_trial": trial,
        }
        body.update(overrides)
        return self.api.purchase(**body)

    def buy_ok(self, product_id="premium_monthly", trial=False, **overrides):
        resp = self.buy(product_id, trial, **overrides)
        assert resp.status_code == 201, resp.text
        return resp.json()["subscription"]

    def notify(self, notification_type, transaction_id, notification_id=None, **extra):
        notification_id = notification_id or f"n-{self.user_id}-{next(_notification_counter)}"
        return self.api.notify(
            notification_id=notification_id,
            type=notification_type,
            original_transaction_id=transaction_id,
            **extra,
        )

    def notify_ok(self, notification_type, transaction_id, **extra):
        resp = self.notify(notification_type, transaction_id, **extra)
        assert resp.status_code == 200, resp.text
        return resp.json()

    def entitlement(self):
        resp = self.api.entitlements(self.user_id)
        assert resp.status_code == 200, resp.text
        return resp.json()

    def events(self, event_type=None):
        return self.api.events(self.user_id, event_type)

    def event_types(self):
        return [e["event_type"] for e in self.events()]
