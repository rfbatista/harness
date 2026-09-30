package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// --- Tickets ---

func (h *Handler) handleListTickets(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	tickets, err := h.planningSvc.ListTickets(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"tickets": tickets})
}

func (h *Handler) handleGetTicket(c echo.Context) error {
	tk, err := h.planningSvc.GetTicket(c.Request().Context(), c.QueryParam("ticket_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"ticket": tk})
}

func (h *Handler) handleCreateTicket(c echo.Context) error {
	var in struct {
		ProjectID   string `json:"project_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	tk, err := h.planningSvc.CreateTicket(c.Request().Context(), in.ProjectID, in.Title, in.Description, domain.TicketStatus(in.Status))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"ticket": tk})
}

func (h *Handler) handleUpdateTicket(c echo.Context) error {
	var in struct {
		TicketID    string `json:"ticket_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	tk, err := h.planningSvc.UpdateTicket(c.Request().Context(), in.TicketID, in.Title, in.Description, domain.TicketStatus(in.Status))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"ticket": tk})
}

func (h *Handler) handleDeleteTicket(c echo.Context) error {
	var in struct {
		TicketID string `json:"ticket_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.planningSvc.DeleteTicket(c.Request().Context(), in.TicketID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Documents ---

func (h *Handler) handleListDocuments(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	return c.JSON(http.StatusOK, map[string]any{"documents": h.planningSvc.ListDocuments(projectID)})
}

func (h *Handler) handleGetDocument(c echo.Context) error {
	doc := h.planningSvc.GetDocument(c.QueryParam("document_id"))
	if doc == nil {
		return echo.NewHTTPError(http.StatusNotFound, "document not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"document": doc})
}

func (h *Handler) handleCreateDocument(c echo.Context) error {
	var in struct {
		ProjectID string `json:"project_id"`
		Title     string `json:"title"`
		Content   string `json:"content"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	doc, err := h.planningSvc.CreateDocument(in.ProjectID, in.Title, in.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"document": doc})
}

func (h *Handler) handleUpdateDocument(c echo.Context) error {
	var in struct {
		DocumentID string `json:"document_id"`
		Title      string `json:"title"`
		Content    string `json:"content"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	doc, err := h.planningSvc.UpdateDocument(in.DocumentID, in.Title, in.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"document": doc})
}

func (h *Handler) handleDeleteDocument(c echo.Context) error {
	var in struct {
		DocumentID string `json:"document_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.planningSvc.DeleteDocument(in.DocumentID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Links ---

func (h *Handler) handleLinkDocument(c echo.Context) error {
	var in struct {
		TicketID   string `json:"ticket_id"`
		DocumentID string `json:"document_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.planningSvc.LinkDocument(in.TicketID, in.DocumentID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"ticket_id": in.TicketID, "document_id": in.DocumentID})
}

func (h *Handler) handleUnlinkDocument(c echo.Context) error {
	var in struct {
		TicketID   string `json:"ticket_id"`
		DocumentID string `json:"document_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.planningSvc.UnlinkDocument(in.TicketID, in.DocumentID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleListTicketDocuments(c echo.Context) error {
	ticketID := c.QueryParam("ticket_id")
	if ticketID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "ticket_id is required")
	}
	return c.JSON(http.StatusOK, map[string]any{"documents": h.planningSvc.ListTicketDocuments(ticketID)})
}
