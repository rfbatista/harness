package architecture

import "context"
import "operators-mcp/internal/domain"

// ListZones returns all zones for the given project with resolved rules.
func (s *Service) ListZones(ctx context.Context, projectID string) []*domain.Zone {
	zones := s.zones.ListByProject(ctx, projectID)
	for _, z := range zones {
		s.resolveZoneRules(z)
	}
	return zones
}

// GetZone returns one zone by id with resolved rules, or nil if not found.
func (s *Service) GetZone(zoneID string) *domain.Zone {
	z := s.zones.Get(zoneID)
	s.resolveZoneRules(z)
	return z
}

// CreateZone creates a zone in the given project with the given metadata.
func (s *Service) CreateZone(projectID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error) {
	z, err := s.zones.Create(projectID, name, pattern, purpose, rules, agents)
	if err != nil {
		return nil, err
	}
	s.resolveZoneRules(z)
	return z, nil
}

// UpdateZone updates an existing zone.
func (s *Service) UpdateZone(zoneID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error) {
	if err := s.refuseLayer(zoneID); err != nil {
		return nil, err
	}
	z, err := s.zones.Update(zoneID, name, pattern, purpose, rules, agents)
	if err != nil {
		return nil, err
	}
	s.resolveZoneRules(z)
	return z, nil
}

// resolveZoneRules hydrates rule Prompt objects from their IDs.
func (s *Service) resolveZoneRules(z *domain.Zone) {
	if z == nil || s.prompts == nil {
		return
	}
	for i, r := range z.Rules {
		if p := s.prompts.GetPrompt(r.ID); p != nil {
			z.Rules[i] = *p
		}
	}
}

// AssignPathToZone adds a path to a zone's explicit paths (path is normalized).
func (s *Service) AssignPathToZone(zoneID, path string) (*domain.Zone, error) {
	if err := s.refuseLayer(zoneID); err != nil {
		return nil, err
	}
	return s.zones.AssignPath(zoneID, domain.NormalizePath(path))
}

// UnassignPathFromZone removes a path from a zone's explicit paths (path is normalized). No-op if path was not in explicit paths.
func (s *Service) UnassignPathFromZone(zoneID, path string) (*domain.Zone, error) {
	if err := s.refuseLayer(zoneID); err != nil {
		return nil, err
	}
	return s.zones.UnassignPath(zoneID, domain.NormalizePath(path))
}

// refuseLayer refuses to change a zone that belongs to a repository: it is a
// layer of that repository's architecture, and only the person edits that, in
// the web UI (ADR 0032). Through the zone tools an agent could widen its own
// access, and the zone would drift from the record.
func (s *Service) refuseLayer(zoneID string) error {
	if z := s.zones.Get(zoneID); z != nil && z.RepositoryID != "" {
		return &domain.StructuredError{Code: "ZONE_IS_LAYER", Message: "zone " + z.Name + " is a layer of a repository's architecture: it is edited on the repository's Architecture page"}
	}
	return nil
}
