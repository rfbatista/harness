package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) taskMemories() error {
	if h.taskMemory == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "task memories are not configured")
	}
	return nil
}

func (h *Handler) handleGetTaskMemory(c echo.Context) error {
	if err := h.taskMemories(); err != nil {
		return err
	}
	var in struct {
		TaskID       string `json:"task_id"`
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	v, err := h.taskMemory.GetTaskMemory(c.Request().Context(), in.TaskID, in.RepositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (h *Handler) handleSaveTaskMemory(c echo.Context) error {
	if err := h.taskMemories(); err != nil {
		return err
	}
	var in struct {
		TaskID       string `json:"task_id"`
		RepositoryID string `json:"repository_id"`
		File         string `json:"file"`
		Content      string `json:"content"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	v, err := h.taskMemory.SaveTaskMemory(c.Request().Context(), in.TaskID, in.RepositoryID, in.File, in.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}
