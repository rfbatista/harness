// Package eventbus dispatches domain events between bounded contexts in
// process. It exists for cross-context side effects only; a context that needs
// to read another's data asks through a read port instead.
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.EventPublisher  = (*Bus)(nil)
	_ ports.EventSubscriber = (*Bus)(nil)
)

// Bus is a synchronous dispatcher. Publish runs every handler of every event on
// the caller's goroutine, in subscription order, and returns their errors
// joined. There are no goroutines and no buffering, so the side effects of a
// use case are done — or reported — when the use case returns.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]ports.EventHandler
}

// New returns an empty bus.
func New() *Bus { return &Bus{handlers: map[string][]ports.EventHandler{}} }

// Subscribe registers h for events named eventName.
func (b *Bus) Subscribe(eventName string, h ports.EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventName] = append(b.handlers[eventName], h)
}

// Publish delivers each event to its handlers. A failing handler does not stop
// the others: each subscriber's side effect is independent.
func (b *Bus) Publish(ctx context.Context, events ...domain.Event) error {
	var errs []error
	for _, ev := range events {
		b.mu.RLock()
		hs := b.handlers[ev.EventName()]
		b.mu.RUnlock()
		for _, h := range hs {
			if err := h(ctx, ev); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", ev.EventName(), err))
			}
		}
	}
	return errors.Join(errs...)
}
