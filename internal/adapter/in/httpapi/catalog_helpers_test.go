package httpapi

import "operators-mcp/internal/app/catalog"

// servicesOf serves a wired catalog, and nothing else, over the HTTP API.
func servicesOf(cat catalog.Catalog) Services {
	return Services{
		Projects:     cat.Projects,
		Discovery:    cat.Projects,
		Env:          cat.Projects,
		Architecture: cat.Architecture,
		Agents:       cat.Agents,
		Capabilities: cat.Capabilities,
		Settings:     cat.Settings,
	}
}
