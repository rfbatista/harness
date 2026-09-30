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
