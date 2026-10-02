package httpclient

import (
	"context"
	"net/url"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TicketBoard = (*Planning)(nil)

// Planning is ports.TicketBoard over the HTTP API.
type Planning struct{ c *Client }

// NewPlanning returns the planning adapter over c.
func NewPlanning(c *Client) *Planning { return &Planning{c: c} }

type ticketOut struct {
	Ticket *domain.Ticket `json:"ticket"`
}

func (p *Planning) CreateTicket(ctx context.Context, projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	var out ticketOut
	err := p.c.post(ctx, "/api/create_ticket", map[string]string{
		"project_id": projectID, "title": title, "description": description, "status": string(status),
	}, &out)
	return out.Ticket, err
}

func (p *Planning) GetTicket(ctx context.Context, id string) (*domain.Ticket, error) {
	var out ticketOut
	return out.Ticket, p.c.get(ctx, "/api/get_ticket", url.Values{"ticket_id": {id}}, &out)
}

func (p *Planning) ListTickets(ctx context.Context, projectID string) ([]*domain.Ticket, error) {
	var out struct {
		Tickets []*domain.Ticket `json:"tickets"`
	}
	return out.Tickets, p.c.get(ctx, "/api/list_tickets", url.Values{"project_id": {projectID}}, &out)
}

func (p *Planning) UpdateTicket(ctx context.Context, id, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	var out ticketOut
	err := p.c.post(ctx, "/api/update_ticket", map[string]string{
		"ticket_id": id, "title": title, "description": description, "status": string(status),
	}, &out)
	return out.Ticket, err
}

func (p *Planning) DeleteTicket(ctx context.Context, id string) error {
	return p.c.post(ctx, "/api/delete_ticket", map[string]string{"ticket_id": id}, nil)
}
