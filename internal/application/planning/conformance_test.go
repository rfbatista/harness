package planning_test

import (
	"context"
	"testing"

	"operators-mcp/internal/domain"

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

func TestSessionCommentsConformance(t *testing.T) {
	portstest.SessionCommentsConformance(t, func(t *testing.T) (ports.TaskComments, string, portstest.SessionCommentSeed) {
		f := newResourceFixture(t)
		return f.svc, f.ticketID, func(t *testing.T, tid string, w domain.CommentWriter, body string) *domain.TicketComment {
			c, err := f.svc.AddSessionComment(context.Background(), tid, w, body)
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
	})
}
