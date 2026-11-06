// Package analytics defines the events the backend emits. The shape follows
// Amplitude's HTTP API: event_type, user_id, time (ms), insert_id and a bag of
// event_properties. tracking-plan.yaml is the human-readable spec for them.
package analytics

import (
	"crypto/rand"
	"fmt"
	"time"
)

const (
	PaywallViewed        = "paywall_viewed"
	TrialStarted         = "trial_started"
	SubscriptionStarted  = "subscription_started"
	TrialConverted       = "trial_converted"
	SubscriptionRenewed  = "subscription_renewed"
	SubscriptionExpired  = "subscription_expired"
	SubscriptionRefunded = "subscription_refunded"
	PurchaseRestored     = "purchase_restored"
)

const (
	PlatformIOS     = "ios"
	PlatformAndroid = "android"
	PlatformWeb     = "web"
)

type Event struct {
	InsertID        string         `json:"insert_id"`
	EventType       string         `json:"event_type"`
	UserID          string         `json:"user_id"`
	Time            int64          `json:"time"` // unix milliseconds
	Platform        string         `json:"platform"`
	EventProperties map[string]any `json:"event_properties"`
}

// New builds an event with a fresh insert_id.
func New(eventType, userID, platform string, at time.Time, props map[string]any) Event {
	if props == nil {
		props = map[string]any{}
	}
	return Event{
		InsertID:        newUUID(),
		EventType:       eventType,
		UserID:          userID,
		Time:            at.UnixMilli(),
		Platform:        platform,
		EventProperties: props,
	}
}

// PlatformForStore maps a store name to the platform value used in events.
func PlatformForStore(store string) string {
	if store == "play" {
		return PlatformAndroid
	}
	return PlatformIOS
}

// newUUID returns a random version 4 UUID.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failing is not recoverable
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// NewID returns a random UUID string, exported for callers that need ids.
func NewID() string { return newUUID() }
