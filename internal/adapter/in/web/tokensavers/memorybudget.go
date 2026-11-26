package tokensavers

import (
	"context"
	"log/slog"
	"strconv"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// MemoryBudgetSeed is what memoryBudgetOverride starts from: a repository's
// or task's own orçamento de memória (0 while it inherits) and what a session
// there gets.
type MemoryBudgetSeed struct {
	Level          domain.ActivationLevel
	OwnerID        string
	Bytes          int64
	EffectiveBytes int64
}

// ReadMemoryBudget seeds the control for owner; nil when overrides are not
// served or could not be read (the page then goes without it).
func ReadMemoryBudget(ctx context.Context, r ports.MemoryBudgetOverrides, owner domain.ActivationOwner) *MemoryBudgetSeed {
	if r == nil {
		return nil
	}
	got, err := r.MemoryBudgetOverride(ctx, owner)
	if err != nil {
		slog.Warn("memory budget not read", "level", owner.Level, "owner", owner.ID, "err", err)
		return nil
	}
	return &MemoryBudgetSeed{Level: owner.Level, OwnerID: owner.ID, Bytes: got.Bytes, EffectiveBytes: got.EffectiveBytes}
}

func gigabytes(bytes int64) string {
	return strconv.FormatFloat(float64(bytes)/(1<<30), 'f', -1, 64)
}

// Value is the field's content: the owner's own GB, empty while it inherits.
func (s MemoryBudgetSeed) Value() string {
	if s.Bytes == 0 {
		return ""
	}
	return gigabytes(s.Bytes)
}

// Summary says what the owner's sessions get and from where.
func (s MemoryBudgetSeed) Summary() string {
	if s.Bytes == 0 {
		return "Inherits " + gigabytes(s.EffectiveBytes) + " GB"
	}
	return "Uses " + gigabytes(s.Bytes) + " GB"
}

// Lead is the line under the heading, for the seed's level.
func (s MemoryBudgetSeed) Lead() string {
	if s.Level == domain.ActivationLevelRepository {
		return "The most RAM one session in this repository may use, counting every process it starts. Empty inherits the Server page's budget; a task can still set its own. A change reaches sessions started or resumed afterwards."
	}
	return "The most RAM one of this task's sessions may use, counting every process it starts. Empty inherits the repository's, then the Server page's. A change reaches sessions started or resumed afterwards."
}
