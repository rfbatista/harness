package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// AgentBoard serves the Agent board: the seed of every task with a live
// session, and the live session feed (ADR 0068).
type AgentBoard interface {
	// Seed reads the board in one pass: no query per project or task, no git.
	Seed(ctx context.Context) (*domain.BoardSeed, error)
	// Follow follows the live session feed until ctx ends. A follower that
	// falls behind is dropped: its channel closes and the board reads its seed
	// again.
	Follow(ctx context.Context) (<-chan domain.BoardEvent, error)
}
