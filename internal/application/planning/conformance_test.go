package planning_test

import (
	"testing"

	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestTicketBoardConformance(t *testing.T) {
	portstest.TicketBoardConformance(t, func(t *testing.T) (ports.TicketBoard, string) {
		return newService(t)
	})
}
