package httpclient

import (
	"context"
	"net/url"
	"strconv"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.Notifications = (*Notifications)(nil)

// Notifications is the person's inbox over /api/notifications.
type Notifications struct{ c *Client }

// NewNotifications returns the inbox adapter over c.
func NewNotifications(c *Client) *Notifications { return &Notifications{c: c} }

func (n *Notifications) ListNotifications(ctx context.Context, q ports.NotificationQuery) (*ports.NotificationPage, error) {
	v := url.Values{}
	if q.ProjectID != "" {
		v.Set("project_id", q.ProjectID)
	}
	if q.UnreadOnly {
		v.Set("unread", "true")
	}
	if q.Before != "" {
		v.Set("before", q.Before)
	}
	if q.Limit != 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	var page ports.NotificationPage
	if err := n.c.get(ctx, "/api/notifications", v, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (n *Notifications) MarkNotificationsRead(ctx context.Context, req ports.MarkNotificationsRead) (*ports.NotificationChange, error) {
	var out struct {
		Notifications []*domain.Notification `json:"notifications"`
		UnreadCount   int                    `json:"unread_count"`
	}
	if err := n.c.post(ctx, "/api/notifications/read", req, &out); err != nil {
		return nil, err
	}
	return &ports.NotificationChange{Changed: out.Notifications, UnreadCount: out.UnreadCount}, nil
}

// FollowNotifications follows every inbox change until ctx ends or the
// stream does (the server dropped this follower, or the connection was
// lost), and then closes the channel.
func (n *Notifications) FollowNotifications(ctx context.Context) (<-chan ports.NotificationChange, error) {
	return follow(ctx, n.c, "/api/notifications/events", func(ports.NotificationChange) bool { return true })
}
