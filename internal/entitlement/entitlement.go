// Package entitlement holds the subscription state machine. It has no
// database or HTTP code so the rules can be tested in isolation.
package entitlement

import (
	"errors"
	"fmt"
	"time"
)

type State string

const (
	StateTrial    State = "trial"
	StateActive   State = "active"
	StateExpired  State = "expired"
	StateRefunded State = "refunded"
)

// Event is something the store tells us happened to a subscription.
type Event string

const (
	EventRenewed  Event = "renewed"
	EventExpired  Event = "expired"
	EventRefunded Event = "refunded"
)

// ErrInvalidTransition means the event cannot happen in the current state,
// for example a renewal for a subscription that was already refunded.
var ErrInvalidTransition = errors.New("invalid transition")

// Initial returns the state of a brand new subscription.
func Initial(withTrial bool) State {
	if withTrial {
		return StateTrial
	}
	return StateActive
}

// Next applies an event to a state. changed is false when the event is valid
// but has no effect (for example EXPIRED arriving for a refunded subscription),
// so callers can skip writes and analytics events.
//
//	trial   --renewed--> active   (trial converted)
//	active  --renewed--> active   (renewal, still counts as a change)
//	trial/active --expired--> expired
//	trial/active/expired --refunded--> refunded
//
// refunded is final. An expired subscription cannot be renewed: a returning
// customer buys again and gets a new transaction.
func Next(current State, event Event) (next State, changed bool, err error) {
	switch current {
	case StateTrial, StateActive:
		switch event {
		case EventRenewed:
			return StateActive, true, nil
		case EventExpired:
			return StateExpired, true, nil
		case EventRefunded:
			return StateRefunded, true, nil
		}
	case StateExpired:
		switch event {
		case EventExpired:
			return StateExpired, false, nil
		case EventRefunded:
			return StateRefunded, true, nil
		case EventRenewed:
			return current, false, fmt.Errorf("%w: %s while %s", ErrInvalidTransition, event, current)
		}
	case StateRefunded:
		switch event {
		case EventExpired, EventRefunded:
			return StateRefunded, false, nil
		case EventRenewed:
			return current, false, fmt.Errorf("%w: %s while %s", ErrInvalidTransition, event, current)
		}
	default:
		return current, false, fmt.Errorf("unknown state %q", current)
	}
	return current, false, fmt.Errorf("unknown event %q", event)
}

// HasAccess says whether a subscription currently unlocks premium content.
// The state alone is not enough: an active subscription whose expiry date has
// passed (a missed EXPIRED notification) must not keep access.
func HasAccess(state State, expiresAt, now time.Time) bool {
	if state != StateTrial && state != StateActive {
		return false
	}
	return expiresAt.After(now)
}
