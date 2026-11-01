package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// designSystemBody is a design system as the API answers it.
type designSystemBody struct {
	DesignSystemID        string                    `json:"design_system_id"`
	ProjectID             string                    `json:"project_id"`
	Name                  string                    `json:"name"`
	Slug                  string                    `json:"slug"`
	Description           string                    `json:"description"`
	Exists                bool                      `json:"exists"`
	Version               int                       `json:"version"`
	UpdatedAt             *time.Time                `json:"updated_at,omitempty"`
	UpdatedBy             string                    `json:"updated_by"`
	Record                domain.DesignSystemRecord `json:"record"`
	OpenProposals         int                       `json:"open_proposals"`
	RunningDesignSessions int                       `json:"running_design_sessions"`
}

// designVersionBody is one entry of a design system's version list.
type designVersionBody struct {
	Version    int                      `json:"version"`
	At         time.Time                `json:"at"`
	By         string                   `json:"by"`
	Source     string                   `json:"source"`
	ProposalID string                   `json:"proposal_id,omitempty"`
	Repository *designVersionRepository `json:"repository,omitempty"`
	Message    string                   `json:"message"`
	Summary    domain.DesignSummary     `json:"summary"`
}

func (h *Handler) designSystems() error {
	if h.designs == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "design systems are not configured")
	}
	return nil
}

func (h *Handler) handleGetDesignSystem(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string `json:"design_system_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	ds, err := h.designs.GetDesignSystem(c.Request().Context(), in.DesignSystemID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toDesignSystemBody(ds))
}

func toDesignSystemBody(ds *domain.DesignSystem) designSystemBody {
	b := designSystemBody{
		DesignSystemID: ds.ID, ProjectID: ds.ProjectID, Name: ds.Name, Slug: ds.Slug, Description: ds.Description,
		Exists: ds.Exists, Version: ds.Version, UpdatedBy: ds.UpdatedBy, Record: ds.Record,
		OpenProposals: ds.OpenProposals, RunningDesignSessions: ds.RunningDesignSessions,
	}
	if ds.Exists {
		at := ds.UpdatedAt
		b.UpdatedAt = &at
	}
	return b
}

// designSystemUse is a repository that uses the design system. The export
// state arrives with the export: never_exported until then.
type designSystemUse struct {
	RepositoryID string `json:"repository_id"`
	Name         string `json:"name"`
	ExportState  string `json:"export_state"`
}

// designSystemRow is one entry of a project's list.
type designSystemRow struct {
	DesignSystemID        string            `json:"design_system_id"`
	Name                  string            `json:"name"`
	Slug                  string            `json:"slug"`
	Description           string            `json:"description"`
	CreatedAt             time.Time         `json:"created_at"`
	Version               int               `json:"version"`
	UpdatedAt             time.Time         `json:"updated_at"`
	Repositories          []designSystemUse `json:"repositories"`
	OpenProposals         int               `json:"open_proposals"`
	RunningDesignSessions int               `json:"running_design_sessions"`
}

func (h *Handler) handleListDesignSystems(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	list, err := h.designs.ListDesignSystems(c.Request().Context(), in.ProjectID)
	if err != nil {
		return err
	}
	uses := map[string][]designSystemUse{}
	repos, err := h.projects.ListRepositories(c.Request().Context(), in.ProjectID)
	if err != nil {
		return err
	}
	for _, r := range repos {
		if r.DesignSystemID != nil {
			uses[*r.DesignSystemID] = append(uses[*r.DesignSystemID], designSystemUse{RepositoryID: r.ID, Name: r.DisplayName(), ExportState: "never_exported"})
		}
	}
	out := make([]designSystemRow, 0, len(list))
	for _, d := range list {
		used := uses[d.ID]
		if used == nil {
			used = []designSystemUse{}
		}
		out = append(out, designSystemRow{
			DesignSystemID: d.ID, Name: d.Name, Slug: d.Slug, Description: d.Description, CreatedAt: d.CreatedAt,
			Version: d.Version, UpdatedAt: d.UpdatedAt, Repositories: used,
			OpenProposals: d.OpenProposals, RunningDesignSessions: d.RunningDesignSessions,
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"design_systems": out})
}

func (h *Handler) handleCreateDesignSystem(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		ProjectID   string `json:"project_id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	id, err := h.designs.CreateDesignSystem(c.Request().Context(), in.ProjectID, in.Name, in.Slug, in.Description)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"design_system_id": id.ID})
}

func (h *Handler) handleUpdateDesignSystem(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string  `json:"design_system_id"`
		Name           *string `json:"name"`
		Description    *string `json:"description"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	ds, err := h.designs.UpdateDesignSystem(c.Request().Context(), in.DesignSystemID, in.Name, in.Description)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"design_system": toDesignSystemBody(ds)})
}

func (h *Handler) handleDeleteDesignSystem(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string `json:"design_system_id"`
		ConfirmName    string `json:"confirm_name"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.designs.DeleteDesignSystem(c.Request().Context(), in.DesignSystemID, in.ConfirmName); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{})
}

func (h *Handler) handleSaveDesignSystem(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string                    `json:"design_system_id"`
		BaseVersion    *int                      `json:"base_version"`
		Record         domain.DesignSystemRecord `json:"record"`
		Message        string                    `json:"message"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.BaseVersion == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "base_version is required"}
	}
	v, err := h.designs.SaveDesignSystem(c.Request().Context(), in.DesignSystemID, *in.BaseVersion, in.Record, in.Message)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]int{"version": v.Version})
}

func (h *Handler) handleRenameDesignToken(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string `json:"design_system_id"`
		BaseVersion    *int   `json:"base_version"`
		Group          string `json:"group"`
		Name           string `json:"name"`
		NewGroup       string `json:"new_group"`
		NewName        string `json:"new_name"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.BaseVersion == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "base_version is required"}
	}
	v, err := h.designs.RenameDesignToken(c.Request().Context(), in.DesignSystemID, *in.BaseVersion, in.Group, in.Name, in.NewGroup, in.NewName)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]int{"version": v.Version})
}

func (h *Handler) handleListDesignSystemVersions(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string `json:"design_system_id"`
		Limit          int    `json:"limit"`
		Before         int    `json:"before"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	versions, err := h.designs.ListDesignSystemVersions(c.Request().Context(), in.DesignSystemID, in.Limit, in.Before)
	if err != nil {
		return err
	}
	out := make([]designVersionBody, 0, len(versions))
	for _, v := range versions {
		out = append(out, designVersionBody{Version: v.Version, At: v.At, By: v.By, Source: v.Source, ProposalID: v.ProposalID, Repository: versionRepository(v), Message: v.Message, Summary: v.Summary})
	}
	return c.JSON(http.StatusOK, map[string]any{"versions": out})
}

func (h *Handler) handleGetDesignSystemVersion(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string `json:"design_system_id"`
		Version        int    `json:"version"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	v, err := h.designs.GetDesignSystemVersion(c.Request().Context(), in.DesignSystemID, in.Version)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"version": v.Version, "at": v.At, "record": v.Record})
}

func (h *Handler) handleStartDesignSession(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string `json:"design_system_id"`
		AgentID        string `json:"agent_id"`
		Prompt         string `json:"prompt"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	id, err := h.designs.StartDesignSession(c.Request().Context(), in.DesignSystemID, in.AgentID, in.Prompt)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"session_id": id})
}

// proposalBody is a proposal as the list answers it.
type proposalBody struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"session_id"`
	Agent          string     `json:"agent"`
	SessionRunning bool       `json:"session_running"`
	State          string     `json:"state"`
	BaseVersion    int        `json:"base_version"`
	Summary        string     `json:"summary"`
	Items          int        `json:"items"`
	Conflicting    int        `json:"conflicting"`
	SubmittedAt    *time.Time `json:"submitted_at"`
	DecidedAt      *time.Time `json:"decided_at"`
}

func toProposalBody(r *ports.DesignProposalRow) proposalBody {
	b := proposalBody{
		ID: r.ID, SessionID: r.SessionID, Agent: r.Agent, SessionRunning: r.SessionRunning, State: r.State,
		BaseVersion: r.BaseVersion, Summary: r.Summary, Items: r.Items, Conflicting: r.Conflicting,
	}
	if !r.SubmittedAt.IsZero() {
		b.SubmittedAt = &r.SubmittedAt
	}
	if !r.DecidedAt.IsZero() {
		b.DecidedAt = &r.DecidedAt
	}
	return b
}

func (h *Handler) handleListDesignSystemProposals(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		DesignSystemID string `json:"design_system_id"`
		State          string `json:"state"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	rows, err := h.designs.ListDesignProposals(c.Request().Context(), in.DesignSystemID, in.State)
	if err != nil {
		return err
	}
	out := make([]proposalBody, 0, len(rows))
	for _, r := range rows {
		out = append(out, toProposalBody(r))
	}
	return c.JSON(http.StatusOK, map[string]any{"proposals": out})
}

func (h *Handler) handleGetDesignSystemProposal(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		ProposalID string `json:"proposal_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	v, err := h.designs.GetDesignProposal(c.Request().Context(), in.ProposalID)
	if err != nil {
		return err
	}
	affected := make([]map[string]any, 0, len(v.AffectedComponents))
	for _, a := range v.AffectedComponents {
		affected = append(affected, map[string]any{"slug": a.Slug, "by_items": a.ByItems})
	}
	items := v.Items
	if items == nil {
		items = []domain.DesignItem{}
	}
	return c.JSON(http.StatusOK, map[string]any{
		"id": v.ID, "design_system_id": v.DesignSystemID, "session_id": v.SessionID, "agent": v.Agent, "session_running": v.SessionRunning,
		"state": v.State, "base_version": v.BaseVersion, "current_version": v.CurrentVersion, "summary": v.Summary,
		"items": items, "affected_components": affected, "proposed_record": v.ProposedRecord,
		"submitted_at": toProposalBody(&v.DesignProposalRow).SubmittedAt, "decided_at": toProposalBody(&v.DesignProposalRow).DecidedAt,
	})
}
