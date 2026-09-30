package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// EventHandler reacts to one domain event.
type EventHandler func(ctx context.Context, ev domain.Event) error

// EventPublisher is how a bounded context announces what happened in it.
// Publishing is synchronous: it returns once every subscriber has run, with
// their errors joined, so the caller can report a failed side effect.
type EventPublisher interface {
	Publish(ctx context.Context, events ...domain.Event) error
}

// EventSubscriber registers handlers for events by name.
type EventSubscriber interface {
	Subscribe(eventName string, h EventHandler)
}

// On registers a handler for one event type, so the handler receives the
// concrete event instead of asserting it.
func On[E domain.Event](sub EventSubscriber, h func(ctx context.Context, ev E) error) {
	var zero E
	sub.Subscribe(zero.EventName(), func(ctx context.Context, ev domain.Event) error {
		e, ok := ev.(E)
		if !ok {
			return nil
		}
		return h(ctx, e)
	})
}
