package app

import (
	"context"
	"net/http"

	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sessionTaskHandler serves each session its task tools at /mcp/task/{id}: the
// general set, or for the session of a product-owner thread the mode's
// allow-list alone (ADR 0109), so the server holds that mode's line.
func sessionTaskHandler(general []domain.Tool, plan *planning.Service, sessions ports.SessionRepository, memory ports.ProductOwnerMemoryTools, holds ...ports.ProductOwnerHolds) http.Handler {
	src := tooling.ProductOwnerSources{Tasks: plan, Changes: plan.StatusHistory, Sessions: sessions, Patcher: plan, Memory: memory}
	if len(holds) > 0 {
		src.Holds = holds[0]
	}
	owner := tooling.ProductOwnerTools(general, src)
	return mcpsession.ModeTaskHandler(general, owner, func(id string) bool {
		sess := sessions.Get(id)
		return sess != nil && sess.Mode == domain.SessionModeProductOwner
	})
}

// heldActionApplier applies a confirmed held action to the board, as the
// product owner of its thread.
type heldActionApplier struct{ plan *planning.Service }

func (a heldActionApplier) ApplyHeldAction(ctx context.Context, by domain.Author, action domain.HeldAction) (any, error) {
	ctx = domain.WithAuthor(ctx, by)
	switch action.Kind {
	case domain.HeldActionArchiveTask:
		if cur, err := a.plan.GetTicket(ctx, action.TaskID); err != nil || cur.Status != domain.TicketStatusDone || cur.ArchivedAt != nil {
			return nil, ports.ErrHeldActionStale
		}
		tk, err := a.plan.ArchiveTicket(ctx, action.TaskID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"task": tk}, nil
	case domain.HeldActionChangeTask:
		cur, err := a.plan.GetTicket(ctx, action.TaskID)
		if err != nil || cur.ArchivedAt != nil ||
			(action.Title != nil && cur.Title != action.BaseTitle) ||
			(action.Description != nil && cur.Description != action.BaseDescription) ||
			(action.Status != nil && string(cur.Status) != action.BaseStatus) {
			return nil, ports.ErrHeldActionStale
		}
		var patch ports.TicketPatch
		patch.Title, patch.Description = action.Title, action.Description
		if action.Status != nil {
			st := domain.TicketStatus(*action.Status)
			patch.Status = &st
		}
		tk, err := a.plan.PatchTicket(ctx, action.TaskID, patch)
		if err != nil {
			return nil, err
		}
		return map[string]any{"task": tk}, nil
	}
	return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "unknown held action " + action.Kind}
}
