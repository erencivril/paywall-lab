-- One row per store transaction chain. original_transaction_id is the id the
-- store keeps stable across renewals.
CREATE TABLE subscriptions (
    original_transaction_id TEXT PRIMARY KEY,
    user_id        TEXT        NOT NULL,
    product_id     TEXT        NOT NULL,
    store          TEXT        NOT NULL CHECK (store IN ('app_store', 'play')),
    sandbox        BOOLEAN     NOT NULL,
    country        CHAR(2)     NOT NULL,
    tier           TEXT        NOT NULL,
    price_cents    INTEGER     NOT NULL CHECK (price_cents > 0),
    currency       TEXT        NOT NULL,
    state          TEXT        NOT NULL CHECK (state IN ('trial', 'active', 'expired', 'refunded')),
    renewal_count  INTEGER     NOT NULL DEFAULT 0,
    started_at     TIMESTAMPTZ NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX subscriptions_user_idx ON subscriptions (user_id);

-- Backstop for double submits: a user can have only one live subscription
-- per product, even if two requests race past the application check.
CREATE UNIQUE INDEX subscriptions_one_live_per_product
    ON subscriptions (user_id, product_id)
    WHERE state IN ('trial', 'active');

-- A user gets one free trial. Claimed with INSERT ... ON CONFLICT DO NOTHING
-- so two concurrent purchases cannot both win.
CREATE TABLE trial_usage (
    user_id TEXT PRIMARY KEY,
    used_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Processed server notifications. The primary key is the idempotency check.
CREATE TABLE store_notifications (
    notification_id         TEXT PRIMARY KEY,
    type                    TEXT        NOT NULL,
    original_transaction_id TEXT        NOT NULL,
    received_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Amplitude-style events. Written in the same transaction as the state change
-- they describe, so an event exists if and only if the change was committed.
CREATE TABLE analytics_events (
    id               BIGSERIAL PRIMARY KEY,
    insert_id        UUID        NOT NULL UNIQUE,
    event_type       TEXT        NOT NULL,
    user_id          TEXT        NOT NULL,
    time_ms          BIGINT      NOT NULL,
    platform         TEXT        NOT NULL,
    event_properties JSONB       NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX analytics_events_user_idx ON analytics_events (user_id, id);
