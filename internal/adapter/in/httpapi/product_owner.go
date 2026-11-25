package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The product owner's API (contract "Harness server ↔ Web UI — product
// owner"): settings and threads.

func (h *Handler) productOwnerReady() error {
	if h.productOwner == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "the product owner is not available on this server"}
	}
	return nil
}

func (h *Handler) handleGetProductOwner(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	v, err := h.productOwner.Settings(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (h *Handler) handleUpdateProductOwner(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	var in struct {
		ProjectID  string  `json:"project_id"`
		Enabled    *bool   `json:"enabled"`
		AgentID    *string `json:"agent_id"`
		Prompt     *string `json:"prompt"`
		DigestTime *string `json:"digest_time"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	v, err := h.productOwner.UpdateSettings(c.Request().Context(), ports.ProductOwnerSettingsUpdate{
		ProjectID: in.ProjectID, Enabled: in.Enabled, AgentID: in.AgentID, Prompt: in.Prompt, DigestTime: in.DigestTime,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

// productOwnerThreadRow is a thread in the project's list.
type productOwnerThreadRow struct {
	ThreadID           string `json:"thread_id"`
	Title              string `json:"title"`
	StartedAt          any    `json:"started_at"`
	LastActivityAt     any    `json:"last_activity_at"`
	LastMessageExcerpt string `json:"last_message_excerpt"`
	TitleSetByPerson   bool   `json:"title_set_by_person"`
	Turn               string `json:"turn"`
	PendingHeldActions int    `json:"pending_held_actions"`
}

func (h *Handler) handleListProductOwnerThreads(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	threads, err := h.productOwner.Threads(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	rows := make([]productOwnerThreadRow, len(threads))
	for i, t := range threads {
		rows[i] = productOwnerThreadRow{t.ID, t.Title, t.StartedAt, t.LastActivityAt, t.LastMessageExcerpt, t.TitleSetByPerson, t.Turn, t.PendingHeldActions}
	}
	return c.JSON(http.StatusOK, map[string]any{"threads": rows})
}

func (h *Handler) handleGetProductOwnerThread(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	page, err := h.productOwner.Thread(c.Request().Context(), c.QueryParam("thread_id"), c.QueryParam("before"))
	if err != nil {
		return err
	}
	t := page.Thread
	items := page.Items
	if items == nil {
		items = []domain.ProductOwnerItem{}
	}
	return c.JSON(http.StatusOK, map[string]any{
		"thread":      map[string]any{"thread_id": t.ID, "project_id": t.ProjectID, "title": t.Title, "turn": t.Turn, "session_id": t.SessionID},
		"items":       items,
		"has_earlier": page.HasEarlier,
	})
}

func (h *Handler) handleStartProductOwnerThread(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	var in struct {
		ProjectID     string   `json:"project_id"`
		Message       string   `json:"message"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	threadID, itemID, err := h.productOwner.StartThread(c.Request().Context(), in.ProjectID, in.Message, in.AttachmentIDs)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"thread_id": threadID, "item_id": itemID})
}

func (h *Handler) handleSendProductOwnerMessage(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	var in struct {
		ThreadID      string   `json:"thread_id"`
		Message       string   `json:"message"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	itemID, err := h.productOwner.SendMessage(c.Request().Context(), in.ThreadID, in.Message, in.AttachmentIDs)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"item_id": itemID})
}

func (h *Handler) handleStopProductOwnerTurn(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	var in struct {
		ThreadID string `json:"thread_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.productOwner.StopTurn(c.Request().Context(), in.ThreadID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{})
}

func (h *Handler) handleRenameProductOwnerThread(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	var in struct {
		ThreadID string `json:"thread_id"`
		Title    string `json:"title"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	t, err := h.productOwner.RenameThread(c.Request().Context(), in.ThreadID, in.Title)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, t)
}

// productOwnerMemory is the product owner's memory, when it offers one.
func (h *Handler) productOwnerMemory() (ports.ProductOwnerMemory, error) {
	if err := h.productOwnerReady(); err != nil {
		return nil, err
	}
	mem, ok := h.productOwner.(ports.ProductOwnerMemory)
	if !ok {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the product owner's memory is not available on this server"}
	}
	return mem, nil
}

func (h *Handler) handleGetProductOwnerMemory(c echo.Context) error {
	mem, err := h.productOwnerMemory()
	if err != nil {
		return err
	}
	entries, err := mem.Memory(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	if entries == nil {
		entries = []ports.MemoryEntryView{}
	}
	return c.JSON(http.StatusOK, map[string]any{"entries": entries})
}

func (h *Handler) handleUpdateMemoryEntry(c echo.Context) error {
	mem, err := h.productOwnerMemory()
	if err != nil {
		return err
	}
	var in struct {
		EntryID string `json:"entry_id"`
		Text    string `json:"text"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	entry, err := mem.UpdateMemoryEntry(c.Request().Context(), in.EntryID, in.Text)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, entry)
}

func (h *Handler) handleDeleteMemoryEntry(c echo.Context) error {
	mem, err := h.productOwnerMemory()
	if err != nil {
		return err
	}
	var in struct {
		EntryID string `json:"entry_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := mem.DeleteMemoryEntry(c.Request().Context(), in.EntryID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{})
}
