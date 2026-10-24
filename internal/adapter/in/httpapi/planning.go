package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// --- Tickets ---

var errArchiveOff = echo.NewHTTPError(http.StatusServiceUnavailable, "task archive not configured")

// handleListTickets lists a project's tasks that are not archived, or with
// archived=true only the archived ones, newest archived first.
func (h *Handler) handleListTickets(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	// parent_id narrows the list to one feature task's children; present but
	// empty, to the top-level tasks; absent, every task of the project.
	// archived=true lists only the archived tasks instead, newest first.
	var filter ports.TicketFilter
	if q := c.QueryParams(); q.Has("parent_id") {
		parentID := q.Get("parent_id")
		filter.ParentID = &parentID
	}
	list := func(ctx context.Context, projectID string) ([]*domain.Ticket, error) {
		return h.planningSvc.ListTickets(ctx, projectID, filter)
	}
	if c.QueryParam("archived") == "true" {
		if h.archive == nil {
			return errArchiveOff
		}
		list = h.archive.ListArchivedTickets
	}
	tickets, err := list(c.Request().Context(), projectID)
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
		ParentID    string `json:"parent_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	tk, err := h.planningSvc.CreateTicket(c.Request().Context(), in.ProjectID, in.Title, in.Description, domain.TicketStatus(in.Status), in.ParentID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"ticket": tk})
}

// handleUpdateTicket is a partial update: a key absent from the body (or null)
// keeps its value. The board sends only ticket_id and status to move a card;
// an editor sends title and description to save text without moving it; a
// parent_id of "" makes the task top-level again.
func (h *Handler) handleUpdateTicket(c echo.Context) error {
	var in struct {
		TicketID    string  `json:"ticket_id"`
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
		ParentID    *string `json:"parent_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	patch := ports.TicketPatch{Title: in.Title, Description: in.Description, ParentID: in.ParentID}
	if in.Status != nil {
		st := domain.TicketStatus(*in.Status)
		patch.Status = &st
	}
	tk, err := h.planningSvc.PatchTicket(c.Request().Context(), in.TicketID, patch)
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

// handleArchiveTicket archives a done task; the person's route alone.
func (h *Handler) handleArchiveTicket(c echo.Context) error {
	return h.setArchived(c, true)
}

// handleRestoreTicket puts an archived task back on the board, still done.
func (h *Handler) handleRestoreTicket(c echo.Context) error {
	return h.setArchived(c, false)
}

func (h *Handler) setArchived(c echo.Context, archive bool) error {
	if h.archive == nil {
		return errArchiveOff
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	set := h.archive.RestoreTicket
	if archive {
		set = h.archive.ArchiveTicket
	}
	tk, err := set(c.Request().Context(), in.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"ticket": tk})
}

// handleArchiveDoneTickets clears the project's Done column: it archives
// every done task still on the board and answers how many and which.
func (h *Handler) handleArchiveDoneTickets(c echo.Context) error {
	if h.archive == nil {
		return errArchiveOff
	}
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	ids, err := h.archive.ArchiveDoneTickets(c.Request().Context(), in.ProjectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, struct {
		Archived int      `json:"archived"`
		IDs      []string `json:"ids"`
	}{len(ids), ids})
}

// --- Documents ---

// handleListDocuments lists a project's documents; scope narrows to task or
// project documents, and an unknown scope is INVALID_INPUT.
func (h *Handler) handleListDocuments(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	scope := domain.DocumentScope(c.QueryParam("scope"))
	if scope != "" && !scope.Valid() {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "scope must be task or project"}
	}
	return c.JSON(http.StatusOK, map[string]any{"documents": h.planningSvc.ListDocuments(projectID, scope)})
}

func (h *Handler) handleGetDocument(c echo.Context) error {
	doc := h.planningSvc.GetDocument(c.QueryParam("document_id"))
	if doc == nil {
		return echo.NewHTTPError(http.StatusNotFound, "document not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"document": doc})
}

// handleCreateDocument creates a document at the project: scope defaults to
// project, format to markdown.
func (h *Handler) handleCreateDocument(c echo.Context) error {
	var in struct {
		ProjectID string `json:"project_id"`
		Title     string `json:"title"`
		Content   string `json:"content"`
		Format    string `json:"format"` // markdown (default), html or text
		Scope     string `json:"scope"`  // project (default) or task
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	doc, err := h.planningSvc.CreateDocument(in.ProjectID, in.Title, in.Content, domain.DocumentFormat(in.Format), domain.DocumentScope(in.Scope))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"document": doc})
}

// handleSetDocumentScope moves a document between task and project scope.
// Ticket links are untouched; the same scope again is fine.
func (h *Handler) handleSetDocumentScope(c echo.Context) error {
	var in struct {
		DocumentID string `json:"document_id"`
		Scope      string `json:"scope"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	doc, err := h.planningSvc.SetDocumentScope(in.DocumentID, domain.DocumentScope(in.Scope))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"document": doc})
}

// handleUpdateDocument replaces title and content; a missing format keeps
// the stored one.
func (h *Handler) handleUpdateDocument(c echo.Context) error {
	var in struct {
		DocumentID string `json:"document_id"`
		Title      string `json:"title"`
		Content    string `json:"content"`
		Format     string `json:"format"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	doc, err := h.planningSvc.UpdateDocument(in.DocumentID, in.Title, in.Content, domain.DocumentFormat(in.Format))
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
	return c.JSON(http.StatusOK, map[string]any{"documents": h.planningSvc.ListTicketDocuments(c.Request().Context(), ticketID)})
}
