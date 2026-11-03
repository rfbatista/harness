package app

import (
	"context"
	"encoding/json"

	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type catalogReadSource interface {
	ReadCatalog() (*domain.CatalogView, error)
}

type catalogSeed struct {
	src        catalogReadSource
	placements ports.AgentPlacements
}

func (s catalogSeed) GetAgentCatalog(context.Context) (json.RawMessage, error) {
	view, err := s.src.ReadCatalog()
	if err != nil {
		return nil, err
	}
	if s.placements != nil {
		if view, err = s.placements.DecorateCatalog(context.Background(), view); err != nil {
			return nil, err
		}
	}
	return json.Marshal(view)
}

// CatalogSeed reads the agent catalog for the Agents page's first paint: the
// bytes GET /api/get_agent_catalog answers.
func CatalogSeed(src catalogReadSource, placements ports.AgentPlacements) web.CatalogReader {
	return catalogSeed{src: src, placements: placements}
}
