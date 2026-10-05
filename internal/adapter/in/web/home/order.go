package home

import (
	"cmp"
	"slices"
	"strings"

	"operators-mcp/internal/domain"
)

// firstByName is the project the rail lists first. projects is non-empty.
func firstByName(projects []*domain.Project) *domain.Project {
	return slices.MinFunc(projects, func(a, b *domain.Project) int {
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}
