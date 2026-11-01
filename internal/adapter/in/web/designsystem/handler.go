// Package designsystem serves a project's Design system page: the shell, its
// tabs and the first-start screen. The record is written by the browser
// through /api (web/src/modules/designsystem); the server seeds it for the
// first paint and leaves every edit to the page.
package designsystem

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is a project's Design system page; tab is "" for its first tab.
func Href(projectID, tab string) string {
	href := "/projects/" + url.PathEscape(projectID) + "/design-system"
	if tab != "" {
		href += "?tab=" + url.QueryEscape(tab)
	}
	return href
}

// tabs are the `?tab=` values the page opens on; anything else opens the first.
var tabs = map[string]bool{"tokens": true, "principles": true, "repositories": true, "history": true}

// Handler serves GET /projects/{project}/design-system.
type Handler struct {
	Projects ports.ProjectReader
	// Designs seeds the record; nil leaves the read to the browser.
	Designs ports.DesignSystemReader
	Layout  shell.Layout
	Render  shell.Renderer
}

// State is the project's design system as get_design_system answers it.
type State struct {
	Exists    bool                      `json:"exists"`
	Version   int                       `json:"version"`
	UpdatedAt string                    `json:"updated_at,omitempty"`
	UpdatedBy string                    `json:"updated_by"`
	Record    domain.DesignSystemRecord `json:"record"`
}

// Seed is what designSystemPage starts from.
type Seed struct {
	ProjectID string `json:"project_id"`
	// Base is the page's own path; the open tab is its ?tab= query.
	Base string `json:"base"`
	// Tab is the tab the address names, "" for the first.
	Tab string `json:"tab"`
	// State is nil when it could not be read: the browser reads it then.
	State *State `json:"state"`
}

// View is what the page renders.
type View struct {
	Frame       shell.Frame
	ProjectID   string
	ProjectName string
	ProjectHref string
	Seed        Seed
}

// Page renders the page. A failed read still renders it, and the browser
// reads the record itself.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	seed := Seed{ProjectID: project.ID, Base: Href(project.ID, "")}
	if tab := r.URL.Query().Get("tab"); tabs[tab] {
		seed.Tab = tab
	}
	if h.Designs != nil {
		// ticket 13 re-keys this: the project's first design system stands in.
		if ds, err := h.Designs.GetProjectDesignSystem(ctx, project.ID); err != nil {
			slog.Warn("design system page: record not read; the browser reads it", "project", project.ID, "err", err)
		} else {
			seed.State = &State{Exists: ds.Exists, Version: ds.Version, UpdatedBy: ds.UpdatedBy, Record: ds.Record}
			if ds.Exists {
				seed.State.UpdatedAt = ds.UpdatedAt.Format(time.RFC3339)
			}
			seed.State.Record.Normalize()
		}
	}
	frame, err := h.Layout(ctx, "Design system · "+project.Name, project.ID, "")
	if err != nil {
		return err
	}
	frame.Live = true // the page follows the project feed
	frame.Rail.Page = shell.PageDesignSystem
	return h.Render(w, r, http.StatusOK, PageView(View{
		Frame: frame, ProjectID: project.ID, ProjectName: project.Name,
		ProjectHref: "/projects/" + url.PathEscape(project.ID), Seed: seed,
	}))
}
