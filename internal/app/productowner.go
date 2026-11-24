package app

import (
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
func sessionTaskHandler(general []domain.Tool, plan *planning.Service, sessions ports.SessionRepository) http.Handler {
	owner := tooling.ProductOwnerTools(general, tooling.ProductOwnerSources{Tasks: plan, Changes: plan.StatusHistory, Sessions: sessions, Patcher: plan})
	return mcpsession.ModeTaskHandler(general, owner, func(id string) bool {
		sess := sessions.Get(id)
		return sess != nil && sess.Mode == domain.SessionModeProductOwner
	})
}
