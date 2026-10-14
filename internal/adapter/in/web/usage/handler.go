// Package usage is a project's Usage page, /projects/{project}/usage: token
// usage as the LLM proxy measured it (see "Spec: harness — token usage
// monitoring and live dashboard"). The server seeds today's summary and the
// recent records; the browser's usagePage
// (web/src/modules/usage/presentation/components/usagePage.js) renders them,
// switches ranges and follows the project's usage stream.
package usage

import (
	"log/slog"
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is a project's Usage page.
func Href(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/usage"
}

// Handler serves GET /projects/{project}/usage.
type Handler struct {
	Projects ports.ProjectReader
	Usage    ports.UsageDashboard
	Layout   shell.Layout
	Render   shell.Renderer
}

// Seed is what usagePage starts from, in the usage API's shapes. Summary is
// nil when it could not be read: the browser reads it then.
type Seed struct {
	ProjectID string                `json:"project_id"`
	Summary   *ports.UsageSummary   `json:"summary"`
	Records   []*domain.UsageRecord `json:"records"`
}

// Page renders the page. A failed usage read still renders it, with what
// was read.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	seed := Seed{ProjectID: project.ID, Records: []*domain.UsageRecord{}}
	if sum, err := h.Usage.UsageSummary(ctx, project.ID, ports.UsageToday); err != nil {
		slog.Warn("usage page: summary not read; the browser reads it", "project", project.ID, "err", err)
	} else {
		seed.Summary = sum
	}
	if page, err := h.Usage.ListRecentUsage(ctx, ports.UsageQuery{ProjectID: project.ID}); err != nil {
		slog.Warn("usage page: recent records not read", "project", project.ID, "err", err)
	} else if page.Records != nil {
		seed.Records = page.Records
	}
	frame, err := h.Layout(ctx, "Usage", project.ID, "")
	if err != nil {
		return err
	}
	frame.Rail.Page = shell.PageUsage
	return h.Render(w, r, http.StatusOK, Page(frame, project.ID, project.Name, seed))
}
