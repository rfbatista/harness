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

func TestTaskResourcesConformance(t *testing.T) {
	portstest.TaskResourcesConformance(t, func(t *testing.T) (ports.TaskResources, string) {
		f := newResourceFixture(t)
		return f.svc, f.ticketID
	})
}

func TestTaskCommentsConformance(t *testing.T) {
	portstest.TaskCommentsConformance(t, func(t *testing.T) (ports.TaskComments, string) {
		f := newResourceFixture(t)
		return f.svc, f.ticketID
	})
}
