"""Expected prices and trials per tier.

This is written by hand on purpose and does not read config/remote_config.json.
If someone edits the config, these tests should fail until the price book is
updated too. That is the check that a price change was intended.
"""

PRICES = {
    "tier1": {"premium_monthly": 999, "premium_annual": 5999},
    "ex_tier1": {"premium_monthly": 499, "premium_annual": 2999},
}
TRIAL_DAYS = {"tier1": 7, "ex_tier1": 3}
CURRENCY = "USD"

COUNTRY_TIER = {
    # tier1
    "US": "tier1", "CA": "tier1", "GB": "tier1", "AU": "tier1", "DE": "tier1",
    "FR": "tier1", "NL": "tier1", "SE": "tier1", "CH": "tier1", "JP": "tier1",
    # everything else
    "BR": "ex_tier1", "IN": "ex_tier1", "TR": "ex_tier1", "MX": "ex_tier1",
    "ZA": "ex_tier1", "ID": "ex_tier1", "PL": "ex_tier1", "EG": "ex_tier1",
}

# Variants from the shipped config, pinned so that a change to the hashing or
# the split, which would move users between variants, gets noticed.
PINNED_VARIANTS = {
    "golden-1": "annual_first",
    "golden-2": "control",
    "golden-3": "annual_first",
    "golden-4": "control",
}

PRODUCTS = ["premium_monthly", "premium_annual"]


def display_price(cents):
    return f"${cents // 100}.{cents % 100:02d}"
