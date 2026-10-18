// Package tokensavers renders the Token savers control the project's
// Settings page, the repository's settings page and the task's detail
// share: each token saver's activation at that level and what it resolves
// to. Writes go from the browser to /api (web/src/modules/tokensavers).
package tokensavers

import (
	"context"
	"log/slog"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Seed is what tokenSaversActivations starts from: GET
// /api/list_token_saver_activations' answer.
type Seed struct {
	Level       domain.ActivationLevel        `json:"level"`
	OwnerID     string                        `json:"owner_id"`
	Activations []domain.TokenSaverActivation `json:"activations"`
}

// Read seeds the control for owner; nil when activations are not served or
// could not be read (the page then goes without it).
func Read(ctx context.Context, r ports.TokenSaverActivationReader, owner domain.ActivationOwner) *Seed {
	if r == nil {
		return nil
	}
	list, err := r.ListTokenSaverActivations(ctx, owner)
	if err != nil {
		slog.Warn("token savers not read", "level", owner.Level, "owner", owner.ID, "err", err)
		return nil
	}
	return &Seed{Level: owner.Level, OwnerID: owner.ID, Activations: list}
}

// Choice is one value a level offers.
type Choice struct{ Value, Label string }

// Choices are the values the seed's level takes: a project is on or off, a
// repository or task may also inherit.
func (s Seed) Choices() []Choice {
	on := []Choice{{string(domain.ActivationOn), "on"}, {string(domain.ActivationOff), "off"}}
	if s.Level == domain.ActivationLevelProject {
		return on
	}
	return append([]Choice{{string(domain.ActivationInherit), "inherit"}}, on...)
}

// Lead is the line under the heading, for the seed's level.
func (s Seed) Lead() string {
	switch s.Level {
	case domain.ActivationLevelProject:
		return "Tools that cut the tokens agent sessions spend. On here turns one on for every session of the project, unless a repository or task says otherwise. A change reaches sessions started afterwards."
	case domain.ActivationLevelRepository:
		return "Whether sessions in this repository use each token saver. Inherit follows the project; a task can still say otherwise. A change reaches sessions started afterwards."
	default:
		return "Whether this task's sessions use each token saver. Inherit follows the session's repository, then the project. A change reaches sessions started afterwards."
	}
}
