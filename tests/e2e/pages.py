from playwright.sync_api import Page, expect


class PaywallPage:
    """What a user sees on /paywall, found by data-testid."""

    def __init__(self, page: Page):
        self.page = page
        self.root = page.locator("#paywall")
        self.title = page.get_by_test_id("paywall-title")
        self.trial_text = page.get_by_test_id("trial-text")
        self.cta = page.get_by_test_id("cta")
        self.restore = page.get_by_test_id("restore")
        self.status = page.get_by_test_id("status")
        self.error = page.get_by_test_id("error")
        self.meta = page.get_by_test_id("meta")

    def open(self, country, user_id):
        self.page.goto(f"/paywall?country={country}&user_id={user_id}")
        return self

    def wait_until_ready(self):
        expect(self.root).to_have_attribute("data-state", "ready")
        return self

    def product(self, product_id):
        return self.page.get_by_test_id(f"product-{product_id}")

    def price(self, product_id):
        return self.page.get_by_test_id(f"price-{product_id}")

    def badge(self, product_id):
        return self.page.get_by_test_id(f"badge-{product_id}")
