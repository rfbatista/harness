package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// The person's agent sandbox configuration (ADR 0017): the switch and the
// extra allowlist paths. Every endpoint answers the whole state, which is
// what the Agent isolation page seeds.

var errSandboxSettingsOff = echo.NewHTTPError(http.StatusServiceUnavailable, "sandbox settings not configured")

type sandboxSettingsOut struct {
	Enabled bool `json:"enabled"`
	// Driver names the mechanism that confines agents; empty when the
	// platform has none.
	Driver string           `json:"driver"`
	Paths  []sandboxPathOut `json:"paths"`
}

// sandboxPathOut is an extra path with what is worth a second look about it
// ("missing", "credentials"); the warnings never block saving.
type sandboxPathOut struct {
	ports.SandboxPath
	Warnings []string `json:"warnings"`
}

func (h *Handler) sandboxState(c echo.Context) error {
	out := sandboxSettingsOut{Enabled: h.sandboxSettings.Enabled(), Paths: []sandboxPathOut{}}
	for _, p := range h.sandboxSettings.Paths() {
		out.Paths = append(out.Paths, sandboxPathOut{SandboxPath: p, Warnings: h.sandboxSettings.PathWarnings(p.Path)})
	}
	if h.sandboxDriver != nil {
		if d := h.sandboxDriver(); d != nil {
			out.Driver = d.Name()
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) handleGetSandboxSettings(c echo.Context) error {
	if h.sandboxSettings == nil {
		return errSandboxSettingsOff
	}
	return h.sandboxState(c)
}

func (h *Handler) handleSetSandboxEnabled(c echo.Context) error {
	if h.sandboxSettings == nil {
		return errSandboxSettingsOff
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.sandboxSettings.SetEnabled(in.Enabled); err != nil {
		return err
	}
	return h.sandboxState(c)
}

func (h *Handler) handleAddSandboxPath(c echo.Context) error {
	return h.sandboxPathChange(c, func(p ports.SandboxPath) error {
		_, err := h.sandboxSettings.AddPath(p)
		return err
	})
}

func (h *Handler) handleUpdateSandboxPath(c echo.Context) error {
	return h.sandboxPathChange(c, func(p ports.SandboxPath) error {
		_, err := h.sandboxSettings.SetPathAccess(p.Path, p.Write)
		return err
	})
}

func (h *Handler) handleRemoveSandboxPath(c echo.Context) error {
	return h.sandboxPathChange(c, func(p ports.SandboxPath) error {
		_, err := h.sandboxSettings.RemovePath(p.Path)
		return err
	})
}

func (h *Handler) sandboxPathChange(c echo.Context, change func(ports.SandboxPath) error) error {
	if h.sandboxSettings == nil {
		return errSandboxSettingsOff
	}
	var in ports.SandboxPath
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := change(in); err != nil {
		return err
	}
	return h.sandboxState(c)
}
