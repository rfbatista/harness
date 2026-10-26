// Package catalogstore serves the agents, skills and MCP servers that the
// agents and capabilities contexts store through their repository ports from
// the agent catalog (ADR 0030), so the legacy endpoints and tools keep their
// shapes while every write goes through the one catalog module.
//
// An item's id is its name: the catalog has no other key.
package catalogstore

import (
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// errNotFound is the coded error of a missing item of the given kind.
func errNotFound(code, what string) error {
	return &domain.StructuredError{Code: code, Message: what + " not found"}
}

func invalid(field, message string) error {
	return domain.NewCatalogError("INVALID_INPUT", message, map[string]any{"field": field})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}

// editor is what every store needs of the catalog.
type editor = ports.CatalogEditor

func isMarkdown(path string) bool { return strings.EqualFold(path, "SKILL.md") }
