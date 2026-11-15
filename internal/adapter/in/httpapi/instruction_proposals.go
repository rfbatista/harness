package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// Instruction-file proposals (contract "Harness server ↔ Web UI —
// instruction-file proposals", ADR 0092).

func (h *Handler) instructionProposalsReady() error {
	if h.instructionProposals == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "instruction-file proposals are not configured")
	}
	return nil
}

type proposalTaskBody struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type proposalSessionBody struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Mode  string `json:"mode"`
}

type instructionProposalBody struct {
	ID           string              `json:"id"`
	RepositoryID string              `json:"repository_id"`
	State        string              `json:"state"`
	Summary      string              `json:"summary"`
	Skills       []string            `json:"skills"`
	Files        int                 `json:"files"`
	Task         proposalTaskBody    `json:"task"`
	Session      proposalSessionBody `json:"session"`
	CreatedAt    time.Time           `json:"created_at"`
	DecidedAt    *time.Time          `json:"decided_at"`
	Note         *string             `json:"note"`
}

func instructionProposalBodyOf(r ports.InstructionProposalRow) instructionProposalBody {
	b := instructionProposalBody{
		ID: r.ID, RepositoryID: r.RepositoryID, State: r.State, Summary: r.Summary, Skills: r.Skills, Files: len(r.Files),
		Task:      proposalTaskBody{ID: r.TaskID, Title: r.TaskTitle},
		Session:   proposalSessionBody{ID: r.SessionID, Agent: r.Agent, Mode: r.Mode},
		CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt,
	}
	if b.Skills == nil {
		b.Skills = []string{}
	}
	if r.Note != "" {
		note := r.Note
		b.Note = &note
	}
	return b
}

type instructionProposalFileBody struct {
	Path           string  `json:"path"`
	Op             string  `json:"op"`
	BaseVersion    int     `json:"base_version"`
	CurrentVersion int     `json:"current_version"`
	Stale          bool    `json:"stale"`
	BaseContent    *string `json:"base_content"`
	Content        *string `json:"content"`
}

func instructionProposalFileBodyOf(f ports.InstructionProposalFileView) instructionProposalFileBody {
	b := instructionProposalFileBody{Path: f.Path, Op: f.Op, BaseVersion: f.BaseVersion, CurrentVersion: f.CurrentVersion, Stale: f.Stale}
	if f.Op != "create" {
		base := f.BaseContent
		b.BaseContent = &base
	}
	if f.Op != "remove" {
		content := f.Content
		b.Content = &content
	}
	return b
}

func (h *Handler) handleListInstructionProposals(c echo.Context) error {
	if err := h.instructionProposalsReady(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		State        string `json:"state"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	rows, open, err := h.instructionProposals.ListInstructionProposals(c.Request().Context(), in.RepositoryID, in.State)
	if err != nil {
		return err
	}
	out := make([]instructionProposalBody, len(rows))
	for i, r := range rows {
		out[i] = instructionProposalBodyOf(r)
	}
	return c.JSON(http.StatusOK, map[string]any{"proposals": out, "open_count": open})
}

func (h *Handler) handleGetInstructionProposal(c echo.Context) error {
	if err := h.instructionProposalsReady(); err != nil {
		return err
	}
	var in struct {
		ProposalID string `json:"proposal_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	row, files, err := h.instructionProposals.GetInstructionProposal(c.Request().Context(), in.ProposalID)
	if err != nil {
		return err
	}
	out := make([]instructionProposalFileBody, len(files))
	for i, f := range files {
		out[i] = instructionProposalFileBodyOf(f)
	}
	return c.JSON(http.StatusOK, map[string]any{"proposal": instructionProposalBodyOf(*row), "files": out})
}
