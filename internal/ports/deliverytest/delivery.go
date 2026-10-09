// Package deliverytest is the contract suite every notification delivery
// provider runs (see "Contract: Notifications ↔ delivery providers"). It
// pins what the notifications service relies on: a stable name, delivering
// any notification the inbox can hold, returning when its context ends, and
// not holding on to what it was given.
package deliverytest

import (
	"context"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Observed answers the ids a provider has delivered so far, in order. A
// provider whose deliveries cannot be observed passes nil.
type Observed func() []string

// returnWithin is how long Deliver may take once its context has ended.
const returnWithin = 2 * time.Second

var nameRule = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// NotificationDeliveryConformance runs the provider contract against a
// fresh provider from newProvider.
func NotificationDeliveryConformance(t *testing.T, newProvider func(t *testing.T) (ports.NotificationDelivery, Observed)) {
	t.Run("a stable name settings can be keyed by", func(t *testing.T) {
		p, _ := newProvider(t)
		name := p.Name()
		if !nameRule.MatchString(name) {
			t.Fatalf("Name() = %q, want [a-z][a-z0-9_-]*", name)
		}
		for i := 0; i < 3; i++ {
			if got := p.Name(); got != name {
				t.Fatalf("Name() = %q, then %q", name, got)
			}
		}
	})

	t.Run("every kind and level is delivered", func(t *testing.T) {
		p, observed := newProvider(t)
		var sent []string
		for _, kind := range []domain.NotificationKind{
			domain.NotificationReviewRequested, domain.NotificationSessionWaiting, domain.NotificationTaskReview,
			domain.NotificationSessionFailed, domain.NotificationAgent,
		} {
			for _, level := range []domain.NotificationLevel{domain.NotificationInfo, domain.NotificationAction} {
				n := Sample(string(kind) + "-" + string(level))
				n.Kind, n.Level = kind, level
				if kind == domain.NotificationAgent {
					n.Source = domain.NotificationFromAgent
				}
				deliver(t, p, n)
				sent = append(sent, n.ID)
			}
		}
		wantObserved(t, observed, sent)
	})

	t.Run("optional fields empty, and text at its limits", func(t *testing.T) {
		p, observed := newProvider(t)
		bare := domain.Notification{
			ID: "bare", ProjectID: "p1", Source: domain.NotificationFromSystem, Kind: domain.NotificationSessionFailed,
			Level: domain.NotificationInfo, Title: "t", Link: "/projects/p1", CreatedAt: time.Now(),
		}
		deliver(t, p, bare)
		long := Sample("long")
		long.Title = strings.Repeat("ç", domain.MaxNotificationTitleLen)
		long.Body = strings.Repeat("日本\n", domain.MaxNotificationBodyLen/3) + "x"
		deliver(t, p, long)
		wantObserved(t, observed, []string{"bare", "long"})
	})

	t.Run("an ended context returns promptly", func(t *testing.T) {
		p, _ := newProvider(t)
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		within(t, "Deliver with a cancelled context", func() { _ = p.Deliver(cancelled, Sample("cancelled")) })

		short, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel2()
		within(t, "Deliver with a context that ends mid-call", func() { _ = p.Deliver(short, Sample("mid-call")) })
	})

	t.Run("what it was given is neither changed nor kept", func(t *testing.T) {
		p, observed := newProvider(t)
		read := time.Now()
		n := Sample("mine")
		n.ReadAt = &read
		before := n
		beforeRead := *n.ReadAt
		deliver(t, p, n)
		if !reflect.DeepEqual(n, before) || !n.ReadAt.Equal(beforeRead) {
			t.Fatalf("after Deliver the caller's notification is %+v, want it unchanged", n)
		}
		// Changing the caller's copy afterwards must not change what was
		// delivered.
		n.ID, *n.ReadAt = "changed", time.Time{}
		wantObserved(t, observed, []string{"mine"})
	})

	t.Run("deliveries are observed in order", func(t *testing.T) {
		p, observed := newProvider(t)
		var sent []string
		for _, id := range []string{"n1", "n2", "n3", "n4"} {
			deliver(t, p, Sample(id))
			sent = append(sent, id)
		}
		wantObserved(t, observed, sent)
	})
}

// Sample is a notification with every field set.
func Sample(id string) domain.Notification {
	return domain.Notification{
		ID: id, ProjectID: "p1", ProjectName: "Alpha", TaskID: "t1", TaskTitle: "Ship it", SessionID: "s1",
		Source: domain.NotificationFromSystem, Kind: domain.NotificationReviewRequested, Level: domain.NotificationAction,
		Title: "Review requested: the plan", Body: "Read the plan and approve it.", Link: "/projects/p1/tasks/t1",
		CreatedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
	}
}

func deliver(t *testing.T, p ports.NotificationDelivery, n domain.Notification) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Deliver(ctx, n); err != nil {
		t.Fatalf("Deliver(%s) = %v", n.ID, err)
	}
}

func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(returnWithin):
		t.Fatalf("%s did not return within %s", what, returnWithin)
	}
}

// wantObserved checks that the provider's deliveries end with want, in
// order, when they can be observed.
func wantObserved(t *testing.T, observed Observed, want []string) {
	t.Helper()
	if observed == nil {
		return
	}
	got := observed()
	if len(got) < len(want) || !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("observed deliveries = %v, want them to end with %v", got, want)
	}
}
