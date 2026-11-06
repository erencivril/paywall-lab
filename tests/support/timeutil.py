from datetime import datetime, timedelta


def parse(ts):
    return datetime.fromisoformat(ts)


def length(sub):
    """How long a subscription period lasts, from started_at to expires_at."""
    return parse(sub["expires_at"]) - parse(sub["started_at"])


def days(n):
    return timedelta(days=n)
