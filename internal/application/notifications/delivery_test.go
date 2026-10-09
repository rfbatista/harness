package notifications_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/notifications/notificationstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/deliverytest"
)

// fake is a provider that records what it delivered. block holds every call
// until it is closed (or the call's context ends); err and panics fail it.
type fake struct {
	name   string
	block  chan struct{}
	err    error
	panics bool
	hang   bool

	mu  sync.Mutex
	got []string
}

func (f *fake) Name() string { return f.name }

func (f *fake) Deliver(ctx context.Context, n domain.Notification) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.hang {
		<-ctx.Done()
		return ctx.Err()
	}
	f.mu.Lock()
	f.got = append(f.got, n.ID)
	f.mu.Unlock()
	if f.panics {
		panic("provider bug")
	}
	return f.err
}

func (f *fake) delivered() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

// waitFor polls until f has delivered n notifications.
func (f *fake) waitFor(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := f.delivered()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider %s delivered %d notifications, want %d", f.name, len(got), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheFakeProviderMeetsTheDeliveryContract(t *testing.T) {
	deliverytest.NotificationDeliveryConformance(t, func(t *testing.T) (ports.NotificationDelivery, deliverytest.Observed) {
		f := &fake{name: "fake"}
		return f, f.delivered
	})
}

// settings is a settings reader a test changes.
type settings struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *settings) Setting(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key]
}

func (s *settings) set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[key] = value
}

// logs captures slog's default logger for the test.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) Enabled(context.Context, slog.Level) bool { return true }
func (l *logs) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	})
	l.mu.Lock()
	l.lines = append(l.lines, b.String())
	l.mu.Unlock()
	return nil
}
func (l *logs) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logs) WithGroup(string) slog.Handler      { return l }

func (l *logs) count(substrings ...string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		all := true
		for _, s := range substrings {
			all = all && strings.Contains(line, s)
		}
		if all {
			n++
		}
	}
	return n
}

func captureLogs(t *testing.T) *logs {
	l := &logs{}
	prev := slog.Default()
	slog.SetDefault(slog.New(l))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return l
}

type deliveryRig struct {
	*notificationstest.Env
	svc      *notifications.Service
	bus      *eventbus.Bus
	settings *settings
	n        int
}

func newDeliveryRig(t *testing.T, providers ...notifications.Provider) *deliveryRig {
	t.Helper()
	e := notificationstest.NewEnv(t)
	st := &settings{}
	svc := e.Service(t, notifications.Options{Providers: providers, Settings: st})
	bus := eventbus.New()
	svc.Subscribe(bus)
	svc.StartDelivery()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		svc.StopDelivery(ctx)
	})
	return &deliveryRig{Env: e, svc: svc, bus: bus, settings: st}
}

// notify posts one notification from a fresh session and returns its id.
func (r *deliveryRig) notify(t *testing.T, level domain.NotificationLevel) string {
	t.Helper()
	r.n++
	n, err := r.svc.Notify(context.Background(), ports.NotifyRequest{SessionID: fmt.Sprintf("s%d", r.n), ProjectID: r.ProjectA, Title: "t", Level: level})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func on(f *fake) notifications.Provider {
	return notifications.Provider{Delivery: f, Defaults: notifications.Defaults{Enabled: true}}
}

func TestEveryEnabledProviderGetsEachNewNotificationItsLevelPasses(t *testing.T) {
	a, b, off, actionOnly := &fake{name: "a"}, &fake{name: "b"}, &fake{name: "off"}, &fake{name: "action-only"}
	r := newDeliveryRig(t, on(a), on(b),
		notifications.Provider{Delivery: off},
		notifications.Provider{Delivery: actionOnly, Defaults: notifications.Defaults{Enabled: true, Level: domain.NotificationAction}})
	info := r.notify(t, domain.NotificationInfo)
	action := r.notify(t, domain.NotificationAction)

	for _, f := range []*fake{a, b} {
		if got := f.waitFor(t, 2); got[0] != info || got[1] != action {
			t.Fatalf("%s delivered %v, want [%s %s]", f.name, got, info, action)
		}
	}
	if got := actionOnly.waitFor(t, 1); len(got) != 1 || got[0] != action {
		t.Fatalf("action-only delivered %v, want only %s", got, action)
	}
	if got := off.delivered(); len(got) != 0 {
		t.Fatalf("a disabled provider delivered %v", got)
	}
}

func TestAProviderGetsTheNotificationWithItsNames(t *testing.T) {
	var got domain.Notification
	done := make(chan struct{})
	cap := &capture{f: func(n domain.Notification) { got = n; close(done) }}
	r2 := newDeliveryRig(t, notifications.Provider{Delivery: cap, Defaults: notifications.Defaults{Enabled: true}})
	tk, err := r2.Tickets.Create(r2.ProjectA, "Ship", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r2.svc.Notify(context.Background(), ports.NotifyRequest{ProjectID: r2.ProjectA, TaskID: tk.ID, Title: "t"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing delivered")
	}
	if got.ProjectName != "Alpha" || got.TaskTitle != "Ship" || got.Link != domain.TaskLink(r2.ProjectA, tk.ID) {
		t.Fatalf("delivered %+v, want the names filled", got)
	}
}

type capture struct{ f func(domain.Notification) }

func (c *capture) Name() string { return "capture" }
func (c *capture) Deliver(_ context.Context, n domain.Notification) error {
	c.f(n)
	return nil
}

func TestSettingsChangeDeliveryWithoutARestart(t *testing.T) {
	a := &fake{name: "a"}
	r := newDeliveryRig(t, on(a))
	r.settings.set(domain.NotificationProviderEnabledKey("a"), "false")
	r.notify(t, domain.NotificationAction)
	r.settings.set(domain.NotificationProviderEnabledKey("a"), "true")
	r.settings.set(domain.NotificationProviderLevelKey("a"), "action")
	r.notify(t, domain.NotificationInfo)
	want := r.notify(t, domain.NotificationAction)
	r.settings.set(domain.NotificationProviderLevelKey("a"), "")
	last := r.notify(t, domain.NotificationInfo)

	if got := a.waitFor(t, 2); len(got) != 2 || got[0] != want || got[1] != last {
		t.Fatalf("delivered %v, want [%s %s]", got, want, last)
	}
}

func TestABlockedProviderDelaysNeitherAnotherNorTheCaller(t *testing.T) {
	stuck, b := &fake{name: "stuck", block: make(chan struct{})}, &fake{name: "b"}
	r := newDeliveryRig(t, on(stuck), on(b))
	defer close(stuck.block)
	start := time.Now()
	for i := 0; i < 5; i++ {
		r.notify(t, domain.NotificationInfo)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Notify took %s with a blocked provider", took)
	}
	b.waitFor(t, 5)
}

func TestFailuresAreLoggedWithTheProviderAndLeaveTheInbox(t *testing.T) {
	l := captureLogs(t)
	failing := &fake{name: "failing", err: errors.New("smtp down")}
	panicking := &fake{name: "panicking", panics: true}
	slow := &fake{name: "slow", hang: true}
	r := newDeliveryRig(t, on(failing), on(panicking), on(slow))
	r.svc.SetDeliverTimeout(50 * time.Millisecond)
	id := r.notify(t, domain.NotificationAction)

	deadline := time.Now().Add(5 * time.Second)
	for _, want := range [][]string{
		{"notification delivery failed", "provider=failing", "notification=" + id, "smtp down"},
		{"notification delivery failed", "provider=panicking", "panic: provider bug"},
		{"notification delivery failed", "provider=slow", "deadline exceeded"},
	} {
		for l.count(want...) == 0 {
			if time.Now().After(deadline) {
				t.Fatalf("no log line with %q", want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	page, err := r.svc.ListNotifications(context.Background(), ports.NotificationQuery{})
	if err != nil || len(page.Notifications) != 1 || page.Notifications[0].ID != id || page.UnreadCount != 1 {
		t.Fatalf("inbox after failed deliveries = %+v, %v", page, err)
	}
	// The panicking provider's worker is still alive.
	r.notify(t, domain.NotificationAction)
	panicking.waitFor(t, 2)
}

func TestAFullQueueDropsOnlyForThatProvider(t *testing.T) {
	l := captureLogs(t)
	stuck, b := &fake{name: "stuck", block: make(chan struct{})}, &fake{name: "b"}
	r := newDeliveryRig(t, on(stuck), on(b))
	// One in flight, 256 queued, and the rest dropped for stuck alone.
	const total = 1 + 256 + 3
	r.notify(t, domain.NotificationInfo)
	// Let the first one reach the worker, so it is in flight, not queued.
	time.Sleep(50 * time.Millisecond)
	for i := 1; i < total; i++ {
		r.notify(t, domain.NotificationInfo)
	}
	b.waitFor(t, total)
	if n := l.count("queue is full", "provider=stuck"); n != 3 {
		t.Fatalf("%d drops logged for stuck, want 3", n)
	}
	if n := l.count("queue is full", "provider=b"); n != 0 {
		t.Fatalf("%d drops logged for b", n)
	}
	close(stuck.block)
	if got := stuck.waitFor(t, total-3); len(got) != total-3 {
		t.Fatalf("stuck delivered %d, want %d", len(got), total-3)
	}
}

func TestOnlyNewNotificationsAreDelivered(t *testing.T) {
	a := &fake{name: "a"}
	r := newDeliveryRig(t, on(a))
	open := domain.ReviewRequestChanged{ReviewID: "rv1", ProjectID: r.ProjectA, Subject: "x", State: domain.ReviewPending}
	ctx := context.Background()
	_ = r.bus.Publish(ctx, open)
	_ = r.bus.Publish(ctx, open)
	answered := open
	answered.State = domain.ReviewApproved
	_ = r.bus.Publish(ctx, answered)
	id := r.notify(t, domain.NotificationInfo)
	if _, err := r.svc.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{All: true}); err != nil {
		t.Fatal(err)
	}
	last := r.notify(t, domain.NotificationInfo)
	a.waitFor(t, 3)
	time.Sleep(20 * time.Millisecond)
	if got := a.delivered(); len(got) != 3 || got[1] != id || got[2] != last {
		t.Fatalf("delivered %v, want the review, %s and %s only", got, id, last)
	}
}

func TestStoppingDrainsTheQueues(t *testing.T) {
	e := notificationstest.NewEnv(t)
	slow := &fake{name: "slow", block: make(chan struct{})}
	svc := e.Service(t, notifications.Options{Providers: []notifications.Provider{on(slow)}})
	svc.StartDelivery()
	for i := 0; i < 5; i++ {
		if _, err := svc.Notify(context.Background(), ports.NotifyRequest{SessionID: fmt.Sprint(i), ProjectID: e.ProjectA, Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(slow.block)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	svc.StopDelivery(ctx)
	if got := slow.delivered(); len(got) != 5 {
		t.Fatalf("after stopping, delivered %d of 5", len(got))
	}
	// Once stopped, nothing more is taken.
	if _, err := svc.Notify(context.Background(), ports.NotifyRequest{SessionID: "late", ProjectID: e.ProjectA, Title: "t"}); err != nil {
		t.Fatal(err)
	}
}

func TestStoppingGivesUpOnAHungProviderWhenItsTimeEnds(t *testing.T) {
	e := notificationstest.NewEnv(t)
	hung := &fake{name: "hung", hang: true}
	svc := e.Service(t, notifications.Options{Providers: []notifications.Provider{on(hung)}})
	svc.StartDelivery()
	if _, err := svc.Notify(context.Background(), ports.NotifyRequest{ProjectID: e.ProjectA, Title: "t"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	svc.StopDelivery(ctx)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("StopDelivery took %s with a hung provider", took)
	}
}

func TestProvidersNeedDistinctValidNames(t *testing.T) {
	e := notificationstest.NewEnv(t)
	for _, providers := range [][]notifications.Provider{
		{on(&fake{name: "log"}), on(&fake{name: "log"})},
		{on(&fake{name: "Bad Name"})},
		{{}},
	} {
		if _, err := notifications.New(e.Repo, e.Readers(), notifications.Options{Providers: providers}); err == nil {
			t.Fatalf("New accepted providers %v", providers)
		}
	}
}
