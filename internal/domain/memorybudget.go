package domain

import (
	"strconv"
	"strings"
)

// The orçamento de memória da sessão (see the Glossary's "Memória das
// sessões", ADR 0008): the RSS ceiling for a session's process tree.

// SettingMemoryBudget is the settings key holding the global default
// orçamento, in bytes. Unset or invalid means DefaultMemoryBudgetBytes.
const SettingMemoryBudget = "memory.budget_bytes"

// DefaultMemoryBudgetBytes is the global orçamento when none is set: 4 GB.
const DefaultMemoryBudgetBytes int64 = 4 << 30

// MemoryBudgetSaver is the name a repository's or task's orçamento override
// is stored under in the token saver activations: the same inherit-or-a-value
// model, the value being the bytes. It is not a token saver and is never
// listed among TokenSavers.
const MemoryBudgetSaver TokenSaver = "memory_budget"

// ParseMemoryBudget reads an orçamento in bytes: a positive whole number.
func ParseMemoryBudget(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, &StructuredError{Code: "INVALID_INPUT", Message: "memory budget must be a positive number of bytes, not " + strconv.Quote(s)}
	}
	return n, nil
}

// ResolveMemoryBudget is the effective orçamento for a session: the task's
// override, else the repository's, else the global value; an override or a
// global that is not a positive number counts as unset, and the global then
// falls back to DefaultMemoryBudgetBytes. The source says which level the
// value came from: "task", "repository" or "global". Each argument is a stored value, ""
// when none.
func ResolveMemoryBudget(task, repository, global string) (bytes int64, source string) {
	for _, c := range []struct{ value, from string }{{task, "task"}, {repository, "repository"}, {global, "global"}} {
		if n, err := ParseMemoryBudget(c.value); err == nil {
			return n, c.from
		}
	}
	return DefaultMemoryBudgetBytes, "global"
}

// MemoryBudgetOverride is one owner's own orçamento and what a session there
// resolves to. Bytes is 0 while the owner inherits.
type MemoryBudgetOverride struct {
	Level          ActivationLevel `json:"level"`
	OwnerID        string          `json:"owner_id"`
	Bytes          int64           `json:"bytes"`
	EffectiveBytes int64           `json:"effective_bytes"`
}
