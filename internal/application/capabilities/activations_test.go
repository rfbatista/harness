package capabilities

import (
	"testing"

	"operators-mcp/internal/domain"
)

// TestResolveActivation walks every combination of a token saver's
// activation at task, repository and project level: the most specific level
// that is not "inherit" wins, and a project that never set it is off.
func TestResolveActivation(t *testing.T) {
	const (
		unset   = domain.ActivationValue("")
		inherit = domain.ActivationInherit
		on      = domain.ActivationOn
		off     = domain.ActivationOff
		task    = domain.ActivationLevelTask
		repo    = domain.ActivationLevelRepository
		project = domain.ActivationLevelProject
	)
	cases := []struct {
		task, repository, project domain.ActivationValue
		want                      domain.ActivationValue
		from                      domain.ActivationLevel
	}{
		// The project decides when both lower levels inherit.
		{inherit, inherit, unset, off, project},
		{inherit, inherit, on, on, project},
		{inherit, inherit, off, off, project},
		// The repository overrides the project.
		{inherit, on, unset, on, repo},
		{inherit, on, on, on, repo},
		{inherit, on, off, on, repo},
		{inherit, off, unset, off, repo},
		{inherit, off, on, off, repo},
		{inherit, off, off, off, repo},
		// The task overrides both.
		{on, inherit, unset, on, task},
		{on, inherit, on, on, task},
		{on, inherit, off, on, task},
		{on, on, unset, on, task},
		{on, on, on, on, task},
		{on, on, off, on, task},
		{on, off, unset, on, task},
		{on, off, on, on, task},
		{on, off, off, on, task},
		{off, inherit, unset, off, task},
		{off, inherit, on, off, task},
		{off, inherit, off, off, task},
		{off, on, unset, off, task},
		{off, on, on, off, task},
		{off, on, off, off, task},
		{off, off, unset, off, task},
		{off, off, on, off, task},
		{off, off, off, off, task},
		// A level never set reads as inherit.
		{unset, unset, on, on, project},
		{unset, off, on, off, repo},
		{unset, unset, unset, off, project},
	}
	for _, c := range cases {
		got, from := Resolve(c.task, c.repository, c.project)
		if got != c.want || from != c.from {
			t.Errorf("Resolve(task %q, repository %q, project %q) = %s (%s), want %s (%s)",
				c.task, c.repository, c.project, got, from, c.want, c.from)
		}
	}
}
