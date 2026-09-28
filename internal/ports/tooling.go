package ports

import "operators-mcp/internal/domain"

// ToolRegistry is the driving port of the tooling service: the code-defined
// tools served over MCP. *tooling.Service satisfies it.
type ToolRegistry interface {
	List() []domain.Tool
	Get(name string) *domain.Tool
	GetByID(id string) *domain.Tool
}
