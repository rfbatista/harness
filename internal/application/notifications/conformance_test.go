package notifications_test

import (
	"testing"

	"operators-mcp/internal/application/notifications/notificationstest"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestNotificationsConformance(t *testing.T) {
	portstest.NotificationsConformance(t, func(t *testing.T) (ports.Notifications, portstest.NotificationsFixture) {
		return notificationstest.New(t)
	})
}
