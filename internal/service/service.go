// Package service holds the business rules. Each public method that changes
// state does it in one database transaction together with its analytics event.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/erencivril/paywall-lab/internal/analytics"
	"github.com/erencivril/paywall-lab/internal/entitlement"
	"github.com/erencivril/paywall-lab/internal/remoteconfig"
	"github.com/erencivril/paywall-lab/internal/store"
)

var (
	ErrUnknownProduct       = errors.New("unknown product")
	ErrAlreadySubscribed    = errors.New("user already has a live subscription for this product")
	ErrTrialUsed            = errors.New("free trial already used by this user")
	ErrTransactionConflict  = errors.New("transaction id belongs to a different purchase")
	ErrNotFound             = errors.New("transaction not found")
	ErrInvalidTransition    = entitlement.ErrInvalidTransition
	ErrMissingCreateDetails = errors.New("SUBSCRIBED for an unknown transaction needs user_id, product_id, store and country")
)

type Service struct {
	cfg   *remoteconfig.Config
	store *store.Store
	now   func() time.Time
}

func New(cfg *remoteconfig.Config, st *store.Store) *Service {
	return &Service{cfg: cfg, store: st, now: time.Now}
}

// Config exposes the loaded remote config to the HTTP layer for validation.
func (s *Service) Config() *remoteconfig.Config { return s.cfg }

func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Second) }

// Ping checks the database connection.
func (s *Service) Ping(ctx context.Context) error { return s.store.Ping(ctx) }

// Events returns stored analytics events for the debug endpoint.
func (s *Service) Events(ctx context.Context, f store.EventFilter) ([]analytics.Event, error) {
	return s.store.Queries().ListEvents(ctx, f)
}

// Paywall builds the paywall for a user and records that it was viewed.
func (s *Service) Paywall(ctx context.Context, country, userID string) (remoteconfig.Paywall, error) {
	pw := s.cfg.Paywall(country, userID)
	ev := analytics.New(analytics.PaywallViewed, userID, analytics.PlatformWeb, s.clock(), map[string]any{
		"country":        pw.Country,
		"tier":           pw.Tier,
		"variant":        pw.Variant,
		"config_version": pw.ConfigVersion,
	})
	err := s.store.InTx(ctx, func(q *store.Queries) error { return q.InsertEvent(ctx, ev) })
	return pw, err
}

// ---- views returned to the API ----

type SubscriptionView struct {
	OriginalTransactionID string    `json:"original_transaction_id"`
	ProductID             string    `json:"product_id"`
	Store                 string    `json:"store"`
	Sandbox               bool      `json:"sandbox"`
	State                 string    `json:"state"`
	Tier                  string    `json:"tier"`
	PriceCents            int       `json:"price_cents"`
	Currency              string    `json:"currency"`
	RenewalCount          int       `json:"renewal_count"`
	StartedAt             time.Time `json:"started_at"`
	ExpiresAt             time.Time `json:"expires_at"`
	HasAccess             bool      `json:"has_access"`
}

type EntitlementView struct {
	UserID        string             `json:"user_id"`
	HasAccess     bool               `json:"has_access"`
	State         string             `json:"state"` // "none" when the user has no subscriptions
	Subscriptions []SubscriptionView `json:"subscriptions"`
}

func (s *Service) subscriptionView(sub store.Subscription) SubscriptionView {
	return SubscriptionView{
		OriginalTransactionID: sub.OriginalTransactionID,
		ProductID:             sub.ProductID,
		Store:                 sub.Store,
		Sandbox:               sub.Sandbox,
		State:                 string(sub.State),
		Tier:                  sub.Tier,
		PriceCents:            sub.PriceCents,
		Currency:              sub.Currency,
		RenewalCount:          sub.RenewalCount,
		StartedAt:             sub.StartedAt,
		ExpiresAt:             sub.ExpiresAt,
		HasAccess:             entitlement.HasAccess(sub.State, sub.ExpiresAt, s.now()),
	}
}

func (s *Service) entitlementView(userID string, subs []store.Subscription) EntitlementView {
	view := EntitlementView{UserID: userID, State: "none", Subscriptions: []SubscriptionView{}}
	for i, sub := range subs {
		sv := s.subscriptionView(sub)
		view.Subscriptions = append(view.Subscriptions, sv)
		// Subscriptions are newest first. The top-level state comes from the
		// first one that grants access, otherwise from the newest one.
		if sv.HasAccess && !view.HasAccess {
			view.HasAccess = true
			view.State = sv.State
		} else if i == 0 {
			view.State = sv.State
		}
	}
	return view
}

// Entitlements returns what a user can currently access.
func (s *Service) Entitlements(ctx context.Context, userID string) (EntitlementView, error) {
	subs, err := s.store.Queries().ListSubscriptions(ctx, userID)
	if err != nil {
		return EntitlementView{}, err
	}
	return s.entitlementView(userID, subs), nil
}

// ---- purchases ----

type PurchaseInput struct {
	UserID        string
	ProductID     string
	Store         string
	Country       string
	Sandbox       bool
	StartTrial    bool
	TransactionID string // optional; lets the client retry safely
}

type PurchaseResult struct {
	Subscription SubscriptionView
	Duplicate    bool
}

func (s *Service) Purchase(ctx context.Context, in PurchaseInput) (PurchaseResult, error) {
	var res PurchaseResult
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		if in.TransactionID != "" {
			existing, err := q.GetSubscription(ctx, in.TransactionID)
			if err == nil {
				if existing.UserID != in.UserID || existing.ProductID != in.ProductID {
					return ErrTransactionConflict
				}
				res = PurchaseResult{Subscription: s.subscriptionView(existing), Duplicate: true}
				return nil
			}
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		sub, err := s.createSubscription(ctx, q, createInput{
			UserID: in.UserID, ProductID: in.ProductID, Store: in.Store, Country: in.Country,
			Sandbox: in.Sandbox, StartTrial: in.StartTrial, TransactionID: in.TransactionID,
		})
		if err != nil {
			return err
		}
		res = PurchaseResult{Subscription: s.subscriptionView(sub)}
		return nil
	})
	return res, err
}

type createInput struct {
	UserID, ProductID, Store, Country, TransactionID string
	Sandbox, StartTrial                              bool
}

// createSubscription is shared by client purchases and SUBSCRIBED notifications.
func (s *Service) createSubscription(ctx context.Context, q *store.Queries, in createInput) (store.Subscription, error) {
	product, ok := s.cfg.Product(in.ProductID)
	if !ok {
		return store.Subscription{}, ErrUnknownProduct
	}
	tier := s.cfg.TierFor(in.Country)
	cents, currency, _ := s.cfg.Price(in.ProductID, tier)

	live, err := q.HasLiveSubscription(ctx, in.UserID, in.ProductID)
	if err != nil {
		return store.Subscription{}, err
	}
	if live {
		return store.Subscription{}, ErrAlreadySubscribed
	}

	now := s.clock()
	period := time.Duration(product.PeriodDays) * 24 * time.Hour
	trialDays := s.cfg.TrialDays(tier)
	withTrial := in.StartTrial && trialDays > 0
	if withTrial {
		claimed, err := q.ClaimTrial(ctx, in.UserID)
		if err != nil {
			return store.Subscription{}, err
		}
		if !claimed {
			return store.Subscription{}, ErrTrialUsed
		}
		period = time.Duration(trialDays) * 24 * time.Hour
	}

	txnID := in.TransactionID
	if txnID == "" {
		txnID = "sim_" + strings.ReplaceAll(analytics.NewID(), "-", "")[:16]
	}
	sub := store.Subscription{
		OriginalTransactionID: txnID,
		UserID:                in.UserID,
		ProductID:             in.ProductID,
		Store:                 in.Store,
		Sandbox:               in.Sandbox,
		Country:               in.Country,
		Tier:                  tier,
		PriceCents:            cents,
		Currency:              currency,
		State:                 entitlement.Initial(withTrial),
		StartedAt:             now,
		ExpiresAt:             now.Add(period),
	}
	if err := q.InsertSubscription(ctx, sub); err != nil {
		if errors.Is(err, store.ErrAlreadySubscribed) {
			return store.Subscription{}, ErrAlreadySubscribed
		}
		return store.Subscription{}, err
	}

	eventType := analytics.SubscriptionStarted
	var extra map[string]any
	if withTrial {
		eventType = analytics.TrialStarted
		extra = map[string]any{"trial_days": trialDays}
	}
	if err := q.InsertEvent(ctx, s.subscriptionEvent(eventType, sub, extra)); err != nil {
		return store.Subscription{}, err
	}
	return sub, nil
}

// subscriptionEvent builds an event with the properties every subscription
// event carries, plus any extras.
func (s *Service) subscriptionEvent(eventType string, sub store.Subscription, extra map[string]any) analytics.Event {
	props := map[string]any{
		"original_transaction_id": sub.OriginalTransactionID,
		"product_id":              sub.ProductID,
		"store":                   sub.Store,
		"sandbox":                 sub.Sandbox,
		"country":                 sub.Country,
		"tier":                    sub.Tier,
		"price_cents":             sub.PriceCents,
		"currency":                sub.Currency,
	}
	for k, v := range extra {
		props[k] = v
	}
	return analytics.New(eventType, sub.UserID, analytics.PlatformForStore(sub.Store), s.clock(), props)
}

// ---- restore ----

type RestoreInput struct {
	UserID string
	Store  string
}

type RestoreResult struct {
	Restored      bool
	RestoredCount int
	Entitlement   EntitlementView
}

// Restore re-checks a user's purchases on one store. Only subscriptions that
// still grant access count as restored.
func (s *Service) Restore(ctx context.Context, in RestoreInput) (RestoreResult, error) {
	var res RestoreResult
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		subs, err := q.ListSubscriptions(ctx, in.UserID)
		if err != nil {
			return err
		}
		res.Entitlement = s.entitlementView(in.UserID, subs)
		for _, sub := range subs {
			if sub.Store == in.Store && entitlement.HasAccess(sub.State, sub.ExpiresAt, s.now()) {
				res.RestoredCount++
			}
		}
		res.Restored = res.RestoredCount > 0
		if !res.Restored {
			return nil
		}
		return q.InsertEvent(ctx, analytics.New(analytics.PurchaseRestored, in.UserID,
			analytics.PlatformForStore(in.Store), s.clock(), map[string]any{
				"store":          in.Store,
				"restored_count": res.RestoredCount,
			}))
	})
	return res, err
}

// ---- store notifications ----

const (
	NotifySubscribed = "SUBSCRIBED"
	NotifyDidRenew   = "DID_RENEW"
	NotifyExpired    = "EXPIRED"
	NotifyRefund     = "REFUND"
)

type NotificationInput struct {
	NotificationID        string
	Type                  string
	OriginalTransactionID string
	ExpiresAt             *time.Time // optional, used by DID_RENEW

	// Only used when SUBSCRIBED arrives for a transaction we have not seen.
	UserID, ProductID, Store, Country string
	Sandbox, StartTrial               bool
}

const (
	StatusApplied   = "applied"
	StatusDuplicate = "duplicate"
	StatusNoChange  = "no_change"
)

type NotificationResult struct {
	NotificationID string `json:"notification_id"`
	Status         string `json:"status"`
	State          string `json:"state,omitempty"`
}

func (s *Service) HandleNotification(ctx context.Context, in NotificationInput) (NotificationResult, error) {
	res := NotificationResult{NotificationID: in.NotificationID}
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		// Recording the id first means a duplicate stops here, and a failure
		// further down rolls the record back so the store can retry.
		isNew, err := q.RecordNotification(ctx, in.NotificationID, in.Type, in.OriginalTransactionID)
		if err != nil {
			return err
		}
		if !isNew {
			res.Status = StatusDuplicate
			return nil
		}

		sub, err := q.GetSubscription(ctx, in.OriginalTransactionID)
		if errors.Is(err, store.ErrNotFound) {
			if in.Type != NotifySubscribed {
				return ErrNotFound
			}
			if in.UserID == "" || in.ProductID == "" || in.Store == "" || in.Country == "" {
				return ErrMissingCreateDetails
			}
			created, err := s.createSubscription(ctx, q, createInput{
				UserID: in.UserID, ProductID: in.ProductID, Store: in.Store, Country: in.Country,
				Sandbox: in.Sandbox, StartTrial: in.StartTrial, TransactionID: in.OriginalTransactionID,
			})
			if err != nil {
				return err
			}
			res.Status, res.State = StatusApplied, string(created.State)
			return nil
		}
		if err != nil {
			return err
		}

		res.State = string(sub.State)
		if in.Type == NotifySubscribed {
			res.Status = StatusNoChange // the purchase call already created it
			return nil
		}
		return s.applyStoreEvent(ctx, q, sub, in, &res)
	})
	return res, err
}

func (s *Service) applyStoreEvent(ctx context.Context, q *store.Queries, sub store.Subscription, in NotificationInput, res *NotificationResult) error {
	var event entitlement.Event
	switch in.Type {
	case NotifyDidRenew:
		event = entitlement.EventRenewed
	case NotifyExpired:
		event = entitlement.EventExpired
	case NotifyRefund:
		event = entitlement.EventRefunded
	default:
		return fmt.Errorf("unsupported notification type %q", in.Type)
	}

	previous := sub.State
	next, changed, err := entitlement.Next(previous, event)
	if err != nil {
		return err
	}
	if !changed {
		res.Status = StatusNoChange
		return nil
	}

	var eventType string
	extra := map[string]any{}
	switch event {
	case entitlement.EventRenewed:
		product, _ := s.cfg.Product(sub.ProductID)
		if in.ExpiresAt != nil {
			sub.ExpiresAt = in.ExpiresAt.UTC()
		} else {
			sub.ExpiresAt = sub.ExpiresAt.Add(time.Duration(product.PeriodDays) * 24 * time.Hour)
		}
		if previous == entitlement.StateTrial {
			eventType = analytics.TrialConverted
		} else {
			sub.RenewalCount++
			eventType = analytics.SubscriptionRenewed
			extra["renewal_count"] = sub.RenewalCount
		}
		extra["expires_at"] = sub.ExpiresAt.UnixMilli()
	case entitlement.EventExpired:
		eventType = analytics.SubscriptionExpired
	case entitlement.EventRefunded:
		eventType = analytics.SubscriptionRefunded
		extra["previous_state"] = string(previous)
	}

	sub.State = next
	if err := q.UpdateSubscription(ctx, sub); err != nil {
		return err
	}
	if err := q.InsertEvent(ctx, s.subscriptionEvent(eventType, sub, extra)); err != nil {
		return err
	}
	res.Status, res.State = StatusApplied, string(next)
	return nil
}
