package capabilities

import (
	"context"
	"strconv"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The orçamento de memória da sessão (ADR 0008): a repository's or task's
// override is stored beside the token saver activations, under
// domain.MemoryBudgetSaver, as the bytes; inherit is the absence of a row.

var (
	_ ports.SessionMemoryBudget   = (*Service)(nil)
	_ ports.MemoryBudgetOverrides = (*Service)(nil)
)

// ResolveMemoryBudget is the orçamento for a session of taskID in
// repositoryID: the task's override, else the repository's, else the global
// setting, else 4 GB. It reads only what is stored, so an id that no longer
// exists counts as inheriting, and it never fails: a store that cannot be
// read leaves the global value.
func (s *Service) ResolveMemoryBudget(ctx context.Context, repositoryID, taskID string) (int64, string) {
	global := ""
	if s.settings != nil {
		global = s.settings.Setting(domain.SettingMemoryBudget)
	}
	return domain.ResolveMemoryBudget(
		s.storedMemoryBudget(ctx, domain.ActivationLevelTask, taskID),
		s.storedMemoryBudget(ctx, domain.ActivationLevelRepository, repositoryID),
		global,
	)
}

// storedMemoryBudget is the owner's stored override, "" when it inherits.
func (s *Service) storedMemoryBudget(ctx context.Context, level domain.ActivationLevel, id string) string {
	if s.activations == nil || id == "" {
		return ""
	}
	stored, err := s.activations.List(ctx, domain.ActivationOwner{Level: level, ID: id})
	if err != nil {
		return ""
	}
	for _, a := range stored {
		if a.Saver == domain.MemoryBudgetSaver {
			return string(a.Value)
		}
	}
	return ""
}

// MemoryBudgetOverride reads owner's own orçamento and what a session there
// resolves to.
func (s *Service) MemoryBudgetOverride(ctx context.Context, owner domain.ActivationOwner) (domain.MemoryBudgetOverride, error) {
	sc, err := s.memoryBudgetScope(ctx, owner)
	if err != nil {
		return domain.MemoryBudgetOverride{}, err
	}
	return s.memoryBudgetOf(ctx, owner, sc), nil
}

// SetMemoryBudgetOverride sets owner's own orçamento in bytes; 0 makes it
// inherit again.
func (s *Service) SetMemoryBudgetOverride(ctx context.Context, owner domain.ActivationOwner, bytes int64) (domain.MemoryBudgetOverride, error) {
	if bytes < 0 {
		return domain.MemoryBudgetOverride{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "memory budget must be a positive number of bytes, or 0 to inherit"}
	}
	sc, err := s.memoryBudgetScope(ctx, owner)
	if err != nil {
		return domain.MemoryBudgetOverride{}, err
	}
	if bytes == 0 {
		err = s.activations.Delete(domain.MemoryBudgetSaver, owner)
	} else {
		err = s.activations.Put(domain.Activation{Saver: domain.MemoryBudgetSaver, Owner: owner, ProjectID: sc.projectID, Value: domain.ActivationValue(strconv.FormatInt(bytes, 10))})
	}
	if err != nil {
		return domain.MemoryBudgetOverride{}, err
	}
	return s.memoryBudgetOf(ctx, owner, sc), nil
}

// memoryBudgetScope places owner in its project. Only a repository or a task
// has an orçamento of its own: there is no project level.
func (s *Service) memoryBudgetScope(ctx context.Context, owner domain.ActivationOwner) (activationScope, error) {
	if owner.Level != domain.ActivationLevelRepository && owner.Level != domain.ActivationLevelTask {
		return activationScope{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "a memory budget is set on a repository or a task, not on " + string(owner.Level)}
	}
	return s.activationScope(ctx, owner)
}

func (s *Service) memoryBudgetOf(ctx context.Context, owner domain.ActivationOwner, sc activationScope) domain.MemoryBudgetOverride {
	own, _ := domain.ParseMemoryBudget(s.storedMemoryBudget(ctx, owner.Level, owner.ID))
	effective, _ := s.ResolveMemoryBudget(ctx, sc.repositoryID, sc.taskID)
	return domain.MemoryBudgetOverride{
		Level: owner.Level, OwnerID: owner.ID, Bytes: own,
		EffectiveBytes: effective,
	}
}
