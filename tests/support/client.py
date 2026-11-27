"""Thin HTTP client for the paywall-lab API. Returns raw responses so tests
can assert on status codes and bodies themselves."""
import hashlib
import hmac
import json
import os
import time

import requests

DEFAULT_TIMEOUT = 10

# Same default as docker-compose.yml. CI and local runs use this dev-only
# value; set NOTIFICATION_SIGNING_SECRET in both places to change it.
DEFAULT_SIGNING_SECRET = "dev-only-notification-secret"


def signing_secret():
    return os.environ.get("NOTIFICATION_SIGNING_SECRET", DEFAULT_SIGNING_SECRET)


def sign_notification(raw_body, secret=None, timestamp=None):
    """Headers for a store notification: HMAC-SHA256 over "<unix>.<raw body>"."""
    ts = str(int(time.time()) if timestamp is None else int(timestamp))
    key = (signing_secret() if secret is None else secret).encode()
    digest = hmac.new(key, ts.encode() + b"." + raw_body, hashlib.sha256).hexdigest()
    return {"X-Paywall-Timestamp": ts, "X-Paywall-Signature": f"v1={digest}"}


class ApiClient:
    def __init__(self, base_url):
        self.base_url = base_url.rstrip("/")
        self.session = requests.Session()

    def request(self, method, path, **kwargs):
        kwargs.setdefault("timeout", DEFAULT_TIMEOUT)
        return self.session.request(method, self.base_url + path, **kwargs)

    def health(self):
        return self.request("GET", "/healthz")

    def paywall(self, country, user_id):
        params = {}
        if country is not None:
            params["country"] = country
        if user_id is not None:
            params["user_id"] = user_id
        return self.request("GET", "/v1/paywall", params=params)

    def purchase(self, **body):
        return self.request("POST", "/v1/purchases", json=body)

    def restore(self, **body):
        return self.request("POST", "/v1/restore", json=body)

    def entitlements(self, user_id):
        return self.request("GET", f"/v1/entitlements/{user_id}")

    def notify(self, **body):
        """Send a correctly signed store notification."""
        raw = json.dumps(body).encode()
        return self.notify_raw(raw, sign_notification(raw))

    def notify_raw(self, raw_body, headers=None):
        """Send exact bytes with exactly the given signature headers (none by default)."""
        all_headers = {"Content-Type": "application/json", **(headers or {})}
        return self.request("POST", "/v1/store-notifications", data=raw_body, headers=all_headers)

    def events(self, user_id, event_type=None):
        params = {"user_id": user_id}
        if event_type:
            params["event_type"] = event_type
        resp = self.request("GET", "/v1/debug/events", params=params)
        resp.raise_for_status()
        return resp.json()["events"]
