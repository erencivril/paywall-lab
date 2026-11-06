package entitlement

import (
	"errors"
	"testing"
	"time"
)

func TestInitial(t *testing.T) {
	if got := Initial(true); got != StateTrial {
		t.Errorf("Initial(true) = %s, want trial", got)
	}
	if got := Initial(false); got != StateActive {
		t.Errorf("Initial(false) = %s, want active", got)
	}
}

func TestNext(t *testing.T) {
	tests := []struct {
		name        string
		from        State
		event       Event
		want        State
		wantChanged bool
		wantInvalid bool
	}{
		{"trial converts on renewal", StateTrial, EventRenewed, StateActive, true, false},
		{"active renews", StateActive, EventRenewed, StateActive, true, false},
		{"trial expires", StateTrial, EventExpired, StateExpired, true, false},
		{"active expires", StateActive, EventExpired, StateExpired, true, false},
		{"trial refunded", StateTrial, EventRefunded, StateRefunded, true, false},
		{"active refunded", StateActive, EventRefunded, StateRefunded, true, false},
		{"expired then refunded", StateExpired, EventRefunded, StateRefunded, true, false},
		{"expired twice is a no-op", StateExpired, EventExpired, StateExpired, false, false},
		{"expired cannot renew", StateExpired, EventRenewed, StateExpired, false, true},
		{"refunded twice is a no-op", StateRefunded, EventRefunded, StateRefunded, false, false},
		{"expire after refund is a no-op", StateRefunded, EventExpired, StateRefunded, false, false},
		{"refunded cannot renew", StateRefunded, EventRenewed, StateRefunded, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := Next(tt.from, tt.event)
			if tt.wantInvalid {
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("err = %v, want ErrInvalidTransition", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want || changed != tt.wantChanged {
				t.Errorf("Next(%s, %s) = (%s, %v), want (%s, %v)",
					tt.from, tt.event, got, changed, tt.want, tt.wantChanged)
			}
		})
	}
}

func TestNextRejectsUnknownInput(t *testing.T) {
	if _, _, err := Next(State("paused"), EventRenewed); err == nil {
		t.Error("unknown state should return an error")
	}
	if _, _, err := Next(StateActive, Event("upgraded")); err == nil {
		t.Error("unknown event should return an error")
	}
	if _, _, err := Next(StateActive, Event("upgraded")); errors.Is(err, ErrInvalidTransition) {
		t.Error("unknown event is a programming error, not an invalid transition")
	}
}

func TestHasAccess(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	tests := []struct {
		name    string
		state   State
		expires time.Time
		want    bool
	}{
		{"trial before expiry", StateTrial, future, true},
		{"active before expiry", StateActive, future, true},
		{"active after expiry", StateActive, past, false},
		{"trial after expiry", StateTrial, past, false},
		{"active exactly at expiry", StateActive, now, false},
		{"expired", StateExpired, future, false},
		{"refunded", StateRefunded, future, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasAccess(tt.state, tt.expires, now); got != tt.want {
				t.Errorf("HasAccess = %v, want %v", got, tt.want)
			}
		})
	}
}
