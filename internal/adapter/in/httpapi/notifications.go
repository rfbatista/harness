package httpapi

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The person's inbox across every project (see "Contract: Harness server ↔
// Web UI — notifications").

var errNotificationsOff = echo.NewHTTPError(http.StatusServiceUnavailable, "notifications not configured")

// notificationsRead is the answer to marking notifications read: the ones
// this call marked, and the global unread count.
type notificationsRead struct {
	Notifications []*domain.Notification `json:"notifications"`
	UnreadCount   int                    `json:"unread_count"`
}

func (h *Handler) handleListNotifications(c echo.Context) error {
	if h.notifications == nil {
		return errNotificationsOff
	}
	q := ports.NotificationQuery{ProjectID: c.QueryParam("project_id"), Before: c.QueryParam("before")}
	switch c.QueryParam("unread") {
	case "true":
		q.UnreadOnly = true
	case "", "false":
	default:
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "unread must be true or false"}
	}
	if s := c.QueryParam("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "limit must be a number from 1 to 200"}
		}
		if n == 0 {
			n = -1 // an explicit 0 is out of range, not the default
		}
		q.Limit = n
	}
	page, err := h.notifications.ListNotifications(c.Request().Context(), q)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, page)
}

func (h *Handler) handleMarkNotificationsRead(c echo.Context) error {
	if h.notifications == nil {
		return errNotificationsOff
	}
	var in ports.MarkNotificationsRead
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	change, err := h.notifications.MarkNotificationsRead(c.Request().Context(), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, notificationsRead{Notifications: change.Changed, UnreadCount: change.UnreadCount})
}

// handleNotificationEvents streams every committed inbox change, across
// projects, as bare `data:` lines of ports.NotificationChange.
func (h *Handler) handleNotificationEvents(c echo.Context) error {
	if h.notifications == nil {
		return errNotificationsOff
	}
	changes, err := h.notifications.FollowNotifications(c.Request().Context())
	if err != nil {
		return err
	}
	return streamSSE(c, changes, func(ch ports.NotificationChange) any { return ch })
}
