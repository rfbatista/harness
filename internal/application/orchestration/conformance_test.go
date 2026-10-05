package orchestration

import (
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestSessionReaderConformance(t *testing.T) {
	portstest.SessionReaderConformance(t, func(t *testing.T) (ports.SessionReader, *domain.Session) {
		svc, _ := newInteractiveService(t)
		sess, _ := startInteractive(t, svc, InteractiveRequest{})
		return svc, sess
	})
}

func TestSessionFeedConformance(t *testing.T) {
	portstest.SessionFeedConformance(t, func(t *testing.T) (ports.SessionFeed, string, func() string) {
		svc, _ := newInteractiveService(t)
		return svc, "p1", func() string {
			sess, _ := startInteractive(t, svc, InteractiveRequest{})
			return sess.ID
		}
	})
}
