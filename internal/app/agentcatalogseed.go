package app

import (
	"context"
	"encoding/json"

	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/domain"
)

type catalogReadSource interface {
	ReadCatalog() (*domain.CatalogView, error)
}

type catalogSeed struct{ src catalogReadSource }

func (s catalogSeed) GetAgentCatalog(context.Context) (json.RawMessage, error) {
	view, err := s.src.ReadCatalog()
	if err != nil {
		return nil, err
	}
	return json.Marshal(view)
}

// CatalogSeed reads the agent catalog for the Agents page's first paint: the
// bytes GET /api/get_agent_catalog answers.
func CatalogSeed(src catalogReadSource) web.CatalogReader { return catalogSeed{src: src} }
