package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/erencivril/paywall-lab/internal/analytics"
	"github.com/erencivril/paywall-lab/internal/entitlement"
)

type Subscription struct {
	OriginalTransactionID string
	UserID                string
	ProductID             string
	Store                 string
	Sandbox               bool
	Country               string
	Tier                  string
	PriceCents            int
	Currency              string
	State                 entitlement.State
	RenewalCount          int
	StartedAt             time.Time
	ExpiresAt             time.Time
}

const subscriptionColumns = `original_transaction_id, user_id, product_id, store, sandbox,
	country, tier, price_cents, currency, state, renewal_count, started_at, expires_at`

func scanSubscription(row pgx.Row) (Subscription, error) {
	var s Subscription
	var state string
	err := row.Scan(&s.OriginalTransactionID, &s.UserID, &s.ProductID, &s.Store, &s.Sandbox,
		&s.Country, &s.Tier, &s.PriceCents, &s.Currency, &state, &s.RenewalCount, &s.StartedAt, &s.ExpiresAt)
	s.State = entitlement.State(state)
	return s, err
}

func (q *Queries) InsertSubscription(ctx context.Context, s Subscription) error {
	_, err := q.db.Exec(ctx, `INSERT INTO subscriptions (`+subscriptionColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		s.OriginalTransactionID, s.UserID, s.ProductID, s.Store, s.Sandbox,
		s.Country, s.Tier, s.PriceCents, s.Currency, string(s.State), s.RenewalCount, s.StartedAt, s.ExpiresAt)
	if isUniqueViolation(err, "subscriptions_one_live_per_product") {
		return ErrAlreadySubscribed
	}
	return err
}

// GetSubscription loads one subscription and locks the row until the
// transaction ends, so concurrent notifications for it run one at a time.
func (q *Queries) GetSubscription(ctx context.Context, transactionID string) (Subscription, error) {
	s, err := scanSubscription(q.db.QueryRow(ctx,
		`SELECT `+subscriptionColumns+` FROM subscriptions WHERE original_transaction_id = $1 FOR UPDATE`,
		transactionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	return s, err
}

func (q *Queries) UpdateSubscription(ctx context.Context, s Subscription) error {
	_, err := q.db.Exec(ctx, `UPDATE subscriptions
		SET state = $2, renewal_count = $3, expires_at = $4, updated_at = now()
		WHERE original_transaction_id = $1`,
		s.OriginalTransactionID, string(s.State), s.RenewalCount, s.ExpiresAt)
	return err
}

// ListSubscriptions returns a user's subscriptions, newest first.
func (q *Queries) ListSubscriptions(ctx context.Context, userID string) ([]Subscription, error) {
	rows, err := q.db.Query(ctx, `SELECT `+subscriptionColumns+` FROM subscriptions
		WHERE user_id = $1 ORDER BY started_at DESC, original_transaction_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (q *Queries) HasLiveSubscription(ctx context.Context, userID, productID string) (bool, error) {
	var exists bool
	err := q.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM subscriptions
		WHERE user_id = $1 AND product_id = $2 AND state IN ('trial', 'active'))`,
		userID, productID).Scan(&exists)
	return exists, err
}

// ClaimTrial records that the user used their free trial. It returns false if
// they had already used it.
func (q *Queries) ClaimTrial(ctx context.Context, userID string) (bool, error) {
	tag, err := q.db.Exec(ctx,
		`INSERT INTO trial_usage (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RecordNotification stores a notification id. It returns false if the id was
// seen before, which is how duplicates are detected.
func (q *Queries) RecordNotification(ctx context.Context, id, typ, transactionID string) (bool, error) {
	tag, err := q.db.Exec(ctx, `INSERT INTO store_notifications (notification_id, type, original_transaction_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, typ, transactionID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (q *Queries) InsertEvent(ctx context.Context, e analytics.Event) error {
	_, err := q.db.Exec(ctx, `INSERT INTO analytics_events
		(insert_id, event_type, user_id, time_ms, platform, event_properties)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.InsertID, e.EventType, e.UserID, e.Time, e.Platform, e.EventProperties)
	return err
}

// EventFilter narrows ListEvents. Empty fields are ignored.
type EventFilter struct {
	UserID    string
	EventType string
	Limit     int
}

// ListEvents returns stored events in insertion order.
func (q *Queries) ListEvents(ctx context.Context, f EventFilter) ([]analytics.Event, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 200
	}
	rows, err := q.db.Query(ctx, `SELECT insert_id::text, event_type, user_id, time_ms, platform, event_properties
		FROM analytics_events
		WHERE ($1 = '' OR user_id = $1) AND ($2 = '' OR event_type = $2)
		ORDER BY id LIMIT $3`, f.UserID, f.EventType, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	out := []analytics.Event{}
	for rows.Next() {
		var e analytics.Event
		if err := rows.Scan(&e.InsertID, &e.EventType, &e.UserID, &e.Time, &e.Platform, &e.EventProperties); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
