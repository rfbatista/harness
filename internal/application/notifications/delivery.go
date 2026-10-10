package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Provider is a delivery provider as the app binds it: the adapter, and what
// applies while its settings are unset.
type Provider struct {
	Delivery ports.NotificationDelivery
	Defaults Defaults
}

// Defaults are a provider's configuration while notifications.<name>.enabled
// and notifications.<name>.level are unset.
type Defaults struct {
	Enabled bool
	// Level is the lowest level delivered; "" is info.
	Level domain.NotificationLevel
}

const (
	// queueSize is how many notifications wait for one provider before the
	// next is dropped for that provider.
	queueSize = 256
	// deliverTimeout bounds one Deliver call.
	deliverTimeout = 30 * time.Second
)

var providerName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// dispatcher fans each new notification out to the providers, each through
// its own queue and worker, so one provider never slows, breaks or reorders
// another, the inbox, or the use case that caused the notification.
type dispatcher struct {
	workers  []*worker
	settings ports.SettingsReader
	timeout  time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// closed is guarded by the service's mu, which every enqueue holds.
	closed bool
}

type worker struct {
	Provider
	name  string
	queue chan domain.Notification
}

func newDispatcher(providers []Provider, settings ports.SettingsReader) (*dispatcher, error) {
	d := &dispatcher{settings: settings, timeout: deliverTimeout}
	d.ctx, d.cancel = context.WithCancel(context.Background())
	seen := map[string]bool{}
	for _, p := range providers {
		if p.Delivery == nil {
			return nil, fmt.Errorf("notification provider: nil delivery")
		}
		name := p.Delivery.Name()
		if !providerName.MatchString(name) {
			return nil, fmt.Errorf("notification provider %q: a name is [a-z][a-z0-9_-]*", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("notification provider %q is bound twice", name)
		}
		seen[name] = true
		d.workers = append(d.workers, &worker{Provider: p, name: name, queue: make(chan domain.Notification, queueSize)})
	}
	return d, nil
}

// enqueue hands n to every provider that is enabled and whose level it
// reaches, without waiting: a full queue drops n for that provider only.
// Settings are read here, at dispatch time, so a change applies to the very
// next notification. The caller holds the service's mu.
func (d *dispatcher) enqueue(n domain.Notification) {
	if d.closed {
		return
	}
	for _, w := range d.workers {
		enabled, min := d.config(w)
		if !enabled || !n.Level.AtLeast(min) {
			continue
		}
		select {
		case w.queue <- n:
		default:
			slog.Warn("notification delivery dropped: the provider's queue is full", "provider", w.name, "notification", n.ID)
		}
	}
}

func (d *dispatcher) config(w *worker) (bool, domain.NotificationLevel) {
	enabled, min := w.Defaults.Enabled, w.Defaults.Level
	if min == "" {
		min = domain.NotificationInfo
	}
	if d.settings == nil {
		return enabled, min
	}
	switch d.settings.Setting(domain.NotificationProviderEnabledKey(w.name)) {
	case "true":
		enabled = true
	case "false":
		enabled = false
	}
	if l, err := domain.ParseNotificationLevel(d.settings.Setting(domain.NotificationProviderLevelKey(w.name))); err == nil {
		min = l
	}
	return enabled, min
}

func (d *dispatcher) start() {
	for _, w := range d.workers {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			for n := range w.queue {
				d.deliver(w, n)
			}
		}()
	}
}

// deliver makes one call, bounded by the timeout; an error, a panic or a
// timeout is logged with the provider's name and nothing else happens.
func (d *dispatcher) deliver(w *worker, n domain.Notification) {
	ctx, cancel := context.WithTimeout(d.ctx, d.timeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("notification delivery failed", "provider", w.name, "notification", n.ID, "err", fmt.Sprintf("panic: %v", r))
		}
	}()
	if err := w.Delivery.Deliver(ctx, n); err != nil {
		slog.Warn("notification delivery failed", "provider", w.name, "notification", n.ID, "err", err)
	}
}

// close stops taking notifications; the workers finish what is queued. The
// caller holds the service's mu.
func (d *dispatcher) close() {
	if d.closed {
		return
	}
	d.closed = true
	for _, w := range d.workers {
		close(w.queue)
	}
}

// wait waits for the workers to drain their queues until ctx ends, then
// cancels the calls still running and waits for them to return.
func (d *dispatcher) wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		d.cancel()
		<-done
	}
	d.cancel()
}
