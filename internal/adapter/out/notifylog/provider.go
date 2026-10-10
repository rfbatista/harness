// Package notifylog is a notification delivery provider that writes each
// notification as one structured log line. It is the example provider, and
// the one the tests bind.
package notifylog

import (
	"context"
	"log/slog"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.NotificationDelivery = (*Provider)(nil)

// Provider logs notifications.
type Provider struct{ log *slog.Logger }

// New returns a provider writing to log.
func New(log *slog.Logger) *Provider { return &Provider{log: log} }

// Name is "log": its settings are notifications.log.enabled and .level.
func (p *Provider) Name() string { return "log" }

// Deliver writes one line for n.
func (p *Provider) Deliver(ctx context.Context, n domain.Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.log.LogAttrs(ctx, slog.LevelInfo, "notification",
		slog.String("id", n.ID), slog.String("project", n.ProjectName), slog.String("task", n.TaskTitle),
		slog.String("kind", string(n.Kind)), slog.String("level", string(n.Level)),
		slog.String("title", n.Title), slog.String("link", n.Link))
	return nil
}
