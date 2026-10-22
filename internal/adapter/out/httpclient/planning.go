package httpclient

import (
	"context"
	"net/url"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.TicketBoard   = (*Planning)(nil)
	_ ports.TicketPatcher = (*Planning)(nil)
)

// Planning is ports.TicketBoard and ports.TicketPatcher over the HTTP API.
type Planning struct{ c *Client }

// NewPlanning returns the planning adapter over c.
func NewPlanning(c *Client) *Planning { return &Planning{c: c} }

type ticketOut struct {
	Ticket *domain.Ticket `json:"ticket"`
}

func (p *Planning) CreateTicket(ctx context.Context, projectID, title, description string, status domain.TicketStatus, parentID string) (*domain.Ticket, error) {
	var out ticketOut
	err := p.c.post(ctx, "/api/create_ticket", map[string]string{
		"project_id": projectID, "title": title, "description": description, "status": string(status), "parent_id": parentID,
	}, &out)
	return out.Ticket, err
}

func (p *Planning) GetTicket(ctx context.Context, id string) (*domain.Ticket, error) {
	var out ticketOut
	return out.Ticket, p.c.get(ctx, "/api/get_ticket", url.Values{"ticket_id": {id}}, &out)
}

func (p *Planning) ListTickets(ctx context.Context, projectID string, filter ports.TicketFilter) ([]*domain.Ticket, error) {
	var out struct {
		Tickets []*domain.Ticket `json:"tickets"`
	}
	q := url.Values{"project_id": {projectID}}
	if filter.ParentID != nil {
		q.Set("parent_id", *filter.ParentID)
	}
	return out.Tickets, p.c.get(ctx, "/api/list_tickets", q, &out)
}

func (p *Planning) UpdateTicket(ctx context.Context, id, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	var out ticketOut
	err := p.c.post(ctx, "/api/update_ticket", map[string]string{
		"ticket_id": id, "title": title, "description": description, "status": string(status),
	}, &out)
	return out.Ticket, err
}

// PatchTicket sends only the fields the patch carries: a nil field is left
// out of the body and keeps its value, a present one ("" included: it
// clears the description or the parent) replaces it, as the handler reads
// them. StatusReason and BySessionID have no place on the wire: over HTTP
// a status moves as the person's.
func (p *Planning) PatchTicket(ctx context.Context, id string, patch ports.TicketPatch) (*domain.Ticket, error) {
	body := map[string]any{"ticket_id": id}
	if patch.Title != nil {
		body["title"] = *patch.Title
	}
	if patch.Description != nil {
		body["description"] = *patch.Description
	}
	if patch.Status != nil {
		body["status"] = string(*patch.Status)
	}
	if patch.ParentID != nil {
		body["parent_id"] = *patch.ParentID
	}
	var out ticketOut
	err := p.c.post(ctx, "/api/update_ticket", body, &out)
	return out.Ticket, err
}

func (p *Planning) DeleteTicket(ctx context.Context, id string) error {
	return p.c.post(ctx, "/api/delete_ticket", map[string]string{"ticket_id": id}, nil)
}
