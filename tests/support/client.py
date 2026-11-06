"""Thin HTTP client for the paywall-lab API. Returns raw responses so tests
can assert on status codes and bodies themselves."""
import requests

DEFAULT_TIMEOUT = 10


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
        return self.request("POST", "/v1/store-notifications", json=body)

    def events(self, user_id, event_type=None):
        params = {"user_id": user_id}
        if event_type:
            params["event_type"] = event_type
        resp = self.request("GET", "/v1/debug/events", params=params)
        resp.raise_for_status()
        return resp.json()["events"]
