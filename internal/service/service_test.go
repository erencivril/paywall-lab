package service

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/erencivril/paywall-lab/internal/analytics"
	"github.com/erencivril/paywall-lab/internal/remoteconfig"
	"github.com/erencivril/paywall-lab/internal/store"
)

// These tests need PostgreSQL. They are skipped when DATABASE_URL is not set,
// so `go test ./...` still works on a machine without a database.
func newTestService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.Connect(ctx, url, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cfg, err := remoteconfig.Load("../../config/remote_config.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := New(cfg, st)
	clock := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return clock }
	return svc, &clock
}

func uniqueUser() string { return "go-test-" + analytics.NewID() }

func eventTypes(t *testing.T, svc *Service, userID string) []string {
	t.Helper()
	events, err := svc.Events(context.Background(), store.EventFilter{UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.EventType
	}
	return types
}

func assertEvents(t *testing.T, svc *Service, userID string, want ...string) {
	t.Helper()
	got := eventTypes(t, svc, userID)
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestTrialToRenewalToRefund(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	user := uniqueUser()

	res, err := svc.Purchase(ctx, PurchaseInput{
		UserID: user, ProductID: "premium_monthly", Store: "app_store", Country: "US", Sandbox: true, StartTrial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := res.Subscription
	if sub.State != "trial" || sub.PriceCents != 999 || sub.Tier != "tier1" {
		t.Fatalf("unexpected subscription: %+v", sub)
	}
	if want := sub.StartedAt.Add(7 * 24 * time.Hour); !sub.ExpiresAt.Equal(want) {
		t.Errorf("trial ends %s, want %s", sub.ExpiresAt, want)
	}

	notify := func(id, typ string) NotificationResult {
		t.Helper()
		r, err := svc.HandleNotification(ctx, NotificationInput{
			NotificationID: id, Type: typ, OriginalTransactionID: sub.OriginalTransactionID,
		})
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		return r
	}

	if r := notify(user+"-n1", NotifyDidRenew); r.State != "active" {
		t.Errorf("after first renewal state = %s", r.State)
	}
	if r := notify(user+"-n2", NotifyDidRenew); r.State != "active" {
		t.Errorf("after second renewal state = %s", r.State)
	}
	notify(user+"-n3", NotifyRefund)

	ent, err := svc.Entitlements(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if ent.HasAccess || ent.State != "refunded" {
		t.Errorf("entitlement after refund = %+v", ent)
	}
	assertEvents(t, svc, user, "trial_started", "trial_converted", "subscription_renewed", "subscription_refunded")
}

func TestDuplicateNotificationEmitsOneEvent(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	user := uniqueUser()

	res, err := svc.Purchase(ctx, PurchaseInput{UserID: user, ProductID: "premium_annual", Store: "play", Country: "BR"})
	if err != nil {
		t.Fatal(err)
	}
	in := NotificationInput{NotificationID: user + "-dup", Type: NotifyExpired, OriginalTransactionID: res.Subscription.OriginalTransactionID}

	// Fire the same notification from several goroutines at once.
	var wg sync.WaitGroup
	statuses := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := svc.HandleNotification(ctx, in)
			if err != nil {
				t.Error(err)
				return
			}
			statuses <- r.Status
		}()
	}
	wg.Wait()
	close(statuses)

	counts := map[string]int{}
	for s := range statuses {
		counts[s]++
	}
	if counts[StatusApplied] != 1 || counts[StatusDuplicate] != 7 {
		t.Errorf("statuses = %v, want 1 applied and 7 duplicate", counts)
	}
	assertEvents(t, svc, user, "subscription_started", "subscription_expired")
}

func TestPurchaseRules(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	user := uniqueUser()
	base := PurchaseInput{UserID: user, ProductID: "premium_monthly", Store: "app_store", Country: "DE", StartTrial: true}

	if _, err := svc.Purchase(ctx, PurchaseInput{UserID: user, ProductID: "nope", Store: "play", Country: "DE"}); !errors.Is(err, ErrUnknownProduct) {
		t.Errorf("unknown product: err = %v", err)
	}

	first, err := svc.Purchase(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Purchase(ctx, base); !errors.Is(err, ErrAlreadySubscribed) {
		t.Errorf("second purchase of same product: err = %v", err)
	}

	// A second product with another trial must be refused, trial is once per user.
	other := base
	other.ProductID = "premium_annual"
	if _, err := svc.Purchase(ctx, other); !errors.Is(err, ErrTrialUsed) {
		t.Errorf("second trial: err = %v", err)
	}
	// ... but the same product without a trial is allowed after the first one ends.
	if _, err := svc.HandleNotification(ctx, NotificationInput{
		NotificationID: user + "-exp", Type: NotifyExpired, OriginalTransactionID: first.Subscription.OriginalTransactionID,
	}); err != nil {
		t.Fatal(err)
	}
	base.StartTrial = false
	again, err := svc.Purchase(ctx, base)
	if err != nil {
		t.Fatalf("repurchase after expiry: %v", err)
	}
	if again.Subscription.State != "active" {
		t.Errorf("repurchase state = %s", again.Subscription.State)
	}
}

func TestClientTransactionIDMakesPurchaseIdempotent(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	user := uniqueUser()
	in := PurchaseInput{UserID: user, ProductID: "premium_monthly", Store: "play", Country: "FR", TransactionID: "txn-" + user}

	first, err := svc.Purchase(ctx, in)
	if err != nil || first.Duplicate {
		t.Fatalf("first purchase: dup=%v err=%v", first.Duplicate, err)
	}
	second, err := svc.Purchase(ctx, in)
	if err != nil || !second.Duplicate {
		t.Fatalf("retry: dup=%v err=%v", second.Duplicate, err)
	}
	in.UserID = uniqueUser()
	if _, err := svc.Purchase(ctx, in); !errors.Is(err, ErrTransactionConflict) {
		t.Errorf("same transaction id for another user: err = %v", err)
	}
	assertEvents(t, svc, user, "subscription_started")
}

func TestInvalidTransitionRollsBackNotification(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	user := uniqueUser()

	res, err := svc.Purchase(ctx, PurchaseInput{UserID: user, ProductID: "premium_monthly", Store: "app_store", Country: "US"})
	if err != nil {
		t.Fatal(err)
	}
	txn := res.Subscription.OriginalTransactionID
	if _, err := svc.HandleNotification(ctx, NotificationInput{NotificationID: user + "-r", Type: NotifyRefund, OriginalTransactionID: txn}); err != nil {
		t.Fatal(err)
	}

	renew := NotificationInput{NotificationID: user + "-late", Type: NotifyDidRenew, OriginalTransactionID: txn}
	if _, err := svc.HandleNotification(ctx, renew); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("renewal after refund: err = %v", err)
	}
	// The failed notification must not be remembered, otherwise a retry would
	// be reported as a duplicate and the problem would stay hidden.
	if _, err := svc.HandleNotification(ctx, renew); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("retry should fail the same way, got %v", err)
	}
	assertEvents(t, svc, user, "subscription_started", "subscription_refunded")
}

func TestRestoreOnlyCountsSubscriptionsWithAccess(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	user := uniqueUser()

	empty, err := svc.Restore(ctx, RestoreInput{UserID: user, Store: "app_store"})
	if err != nil || empty.Restored {
		t.Fatalf("restore with no purchases: %+v err=%v", empty, err)
	}

	res, err := svc.Purchase(ctx, PurchaseInput{UserID: user, ProductID: "premium_monthly", Store: "app_store", Country: "IN"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Restore(ctx, RestoreInput{UserID: user, Store: "app_store"})
	if err != nil || !got.Restored || got.RestoredCount != 1 {
		t.Fatalf("restore: %+v err=%v", got, err)
	}
	wrongStore, _ := svc.Restore(ctx, RestoreInput{UserID: user, Store: "play"})
	if wrongStore.Restored {
		t.Error("restore on the other store should find nothing")
	}

	if _, err := svc.HandleNotification(ctx, NotificationInput{
		NotificationID: user + "-ref", Type: NotifyRefund, OriginalTransactionID: res.Subscription.OriginalTransactionID,
	}); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Restore(ctx, RestoreInput{UserID: user, Store: "app_store"})
	if after.Restored || after.Entitlement.HasAccess {
		t.Errorf("refunded subscription must not be restored: %+v", after)
	}
	assertEvents(t, svc, user, "subscription_started", "purchase_restored", "subscription_refunded")
}

func TestRenewalWithPastExpiryGrantsNoAccess(t *testing.T) {
	svc, clock := newTestService(t)
	ctx := context.Background()
	user := uniqueUser()

	res, err := svc.Purchase(ctx, PurchaseInput{UserID: user, ProductID: "premium_monthly", Store: "app_store", Country: "US"})
	if err != nil {
		t.Fatal(err)
	}
	past := clock.Add(-time.Hour)
	if _, err := svc.HandleNotification(ctx, NotificationInput{
		NotificationID: user + "-old", Type: NotifyDidRenew,
		OriginalTransactionID: res.Subscription.OriginalTransactionID, ExpiresAt: &past,
	}); err != nil {
		t.Fatal(err)
	}
	ent, _ := svc.Entitlements(ctx, user)
	if ent.HasAccess {
		t.Errorf("state is active but expiry is in the past, access should be false: %+v", ent)
	}
}

func TestSubscribedNotificationCreatesUnknownTransaction(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	user := uniqueUser()

	_, err := svc.HandleNotification(ctx, NotificationInput{NotificationID: user + "-a", Type: NotifySubscribed, OriginalTransactionID: "txn-" + user})
	if !errors.Is(err, ErrMissingCreateDetails) {
		t.Fatalf("missing details: err = %v", err)
	}
	r, err := svc.HandleNotification(ctx, NotificationInput{
		NotificationID: user + "-b", Type: NotifySubscribed, OriginalTransactionID: "txn-" + user,
		UserID: user, ProductID: "premium_annual", Store: "play", Country: "CA",
	})
	if err != nil || r.Status != StatusApplied || r.State != "active" {
		t.Fatalf("create via notification: %+v err=%v", r, err)
	}
	assertEvents(t, svc, user, "subscription_started")
}
