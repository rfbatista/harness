// Package designsystem serves a project's Design system page: the shell, its
// tabs and the first-start screen. The record is written by the browser
// through /api (web/src/modules/designsystem); the server seeds it for the
// first paint and leaves every edit to the page.
package designsystem

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// IndexHref is a project's Design systems page.
func IndexHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/design-systems"
}

// Href is one design system's page; tab is "" for its first tab.
func Href(projectID, slug, tab string) string {
	href := IndexHref(projectID) + "/" + url.PathEscape(slug)
	if tab != "" {
		href += "?tab=" + url.QueryEscape(tab)
	}
	return href
}

// tabs are the `?tab=` values the page opens on; anything else opens the first.
var tabs = map[string]bool{"tokens": true, "principles": true, "repositories": true, "history": true}

// Handler serves GET /projects/{project}/design-systems/{slug} and the list at
// GET /projects/{project}/design-systems.
type Handler struct {
	Projects ports.ProjectReader
	// Designs finds the design system by slug and seeds its record; nil
	// leaves every design system page not found.
	Designs ports.DesignSystemPages
	Layout  shell.Layout
	Render  shell.Renderer
}

// State is the design system as get_design_system answers it.
type State struct {
	Exists    bool                      `json:"exists"`
	Version   int                       `json:"version"`
	UpdatedAt string                    `json:"updated_at,omitempty"`
	UpdatedBy string                    `json:"updated_by"`
	Record    domain.DesignSystemRecord `json:"record"`
}

// Seed is what designSystemPage starts from.
type Seed struct {
	ProjectID      string `json:"project_id"`
	DesignSystemID string `json:"design_system_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	// IndexHref is the project's list, where a deleted design system goes back to.
	IndexHref string `json:"index_href"`
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
	IndexHref   string
	Seed        Seed
}

// Page renders the page of the design system the slug names. A failed read of
// its record still renders the page, and the browser reads the record itself.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	found, err := h.find(ctx, project.ID, r.PathValue("slug"))
	if err != nil {
		return err
	}
	seed := Seed{
		ProjectID: project.ID, DesignSystemID: found.ID, Name: found.Name, Slug: found.Slug, Description: found.Description,
		IndexHref: IndexHref(project.ID), Base: Href(project.ID, found.Slug, ""),
	}
	if tab := r.URL.Query().Get("tab"); tabs[tab] {
		seed.Tab = tab
	}
	if ds, err := h.Designs.GetDesignSystem(ctx, found.ID); err != nil {
		slog.Warn("design system page: record not read; the browser reads it", "design_system", found.ID, "err", err)
	} else {
		seed.State = &State{Exists: ds.Exists, Version: ds.Version, UpdatedBy: ds.UpdatedBy, Record: ds.Record}
		if ds.Exists {
			seed.State.UpdatedAt = ds.UpdatedAt.Format(time.RFC3339)
		}
		seed.State.Record.Normalize()
	}
	frame, err := h.Layout(ctx, found.Name+" · Design systems · "+project.Name, project.ID, "")
	if err != nil {
		return err
	}
	frame.Live = true // the page follows the project feed
	frame.Rail.Page = shell.PageDesignSystem
	return h.Render(w, r, http.StatusOK, PageView(View{
		Frame: frame, ProjectID: project.ID, ProjectName: project.Name,
		ProjectHref: "/projects/" + url.PathEscape(project.ID), IndexHref: IndexHref(project.ID), Seed: seed,
	}))
}

// find is the project's design system with the slug, or DESIGN_SYSTEM_NOT_FOUND.
func (h Handler) find(ctx context.Context, projectID, slug string) (*domain.DesignSystemIdentity, error) {
	if h.Designs != nil {
		list, err := h.Designs.ListDesignSystems(ctx, projectID)
		if err != nil {
			return nil, err
		}
		for _, d := range list {
			if d.Slug == slug {
				return &d.DesignSystemIdentity, nil
			}
		}
	}
	return nil, &domain.StructuredError{Code: "DESIGN_SYSTEM_NOT_FOUND", Message: "the project has no design system “" + slug + "”"}
}

// Row is one design system of the list as the page seeds it, in the shape of
// list_design_systems; the repositories using it and its open proposals are read
// by the browser.
type Row struct {
	DesignSystemID string `json:"design_system_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	Version        int    `json:"version"`
	Href           string `json:"-"`
	// Repositories and OpenProposals are what the browser reads next.
	Repositories  []struct{} `json:"repositories"`
	OpenProposals int        `json:"open_proposals"`
}

// ListSeed is what designSystemsPage starts from.
type ListSeed struct {
	ProjectID     string `json:"project_id"`
	IndexHref     string `json:"index_href"`
	DesignSystems []Row  `json:"design_systems"`
}

// ListView is what the list renders.
type ListView struct {
	Frame       shell.Frame
	ProjectName string
	ProjectHref string
	Seed        ListSeed
}

// Index renders the project's design systems by name. The repositories using
// each are read by the browser, which also follows the project feed.
func (h Handler) Index(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	rows := []Row{}
	if h.Designs != nil {
		list, err := h.Designs.ListDesignSystems(ctx, project.ID)
		if err != nil {
			return err
		}
		for _, d := range list {
			rows = append(rows, Row{
				DesignSystemID: d.ID, Name: d.Name, Slug: d.Slug, Description: d.Description, Version: d.Version,
				Href: Href(project.ID, d.Slug, ""), Repositories: []struct{}{},
			})
		}
		sort.SliceStable(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	}
	frame, err := h.Layout(ctx, "Design systems · "+project.Name, project.ID, "")
	if err != nil {
		return err
	}
	frame.Live = true // the list follows the project feed
	frame.Rail.Page = shell.PageDesignSystem
	return h.Render(w, r, http.StatusOK, IndexView(ListView{
		Frame: frame, ProjectName: project.Name, ProjectHref: "/projects/" + url.PathEscape(project.ID),
		Seed: ListSeed{ProjectID: project.ID, IndexHref: IndexHref(project.ID), DesignSystems: rows},
	}))
}
