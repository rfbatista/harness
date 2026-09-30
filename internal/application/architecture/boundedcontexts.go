package architecture

import (
	"context"

	"operators-mcp/internal/domain"
)

var errNoBoundedContexts = &domain.StructuredError{Code: "INTERNAL", Message: "bounded context store not configured"}

// ListBoundedContexts returns all bounded contexts for the given project.
func (s *Service) ListBoundedContexts(projectID string) []*domain.BoundedContext {
	if s.boundedContexts == nil {
		return nil
	}
	return s.boundedContexts.ListByProject(projectID)
}

// GetBoundedContext returns one bounded context by id, or nil if not found.
func (s *Service) GetBoundedContext(id string) *domain.BoundedContext {
	if s.boundedContexts == nil {
		return nil
	}
	return s.boundedContexts.Get(id)
}

// CreateBoundedContext creates a bounded context scoped to a project.
func (s *Service) CreateBoundedContext(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	if s.boundedContexts == nil {
		return nil, errNoBoundedContexts
	}
	if s.projects == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	if _, err := s.projects.GetProject(context.TODO(), projectID); err != nil {
		return nil, err
	}
	return s.boundedContexts.Create(projectID, name, purpose, terms)
}

// UpdateBoundedContext updates an existing bounded context; the ubiquitous
// language is replaced wholesale.
func (s *Service) UpdateBoundedContext(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	if s.boundedContexts == nil {
		return nil, errNoBoundedContexts
	}
	return s.boundedContexts.Update(id, name, purpose, terms)
}

// DeleteBoundedContext deletes a bounded context. Zones linked to it are
// unlinked, not deleted.
func (s *Service) DeleteBoundedContext(id string) error {
	if s.boundedContexts == nil {
		return errNoBoundedContexts
	}
	if s.boundedContexts.Get(id) == nil {
		return &domain.StructuredError{Code: "BOUNDED_CONTEXT_NOT_FOUND", Message: "bounded context not found"}
	}
	if err := s.zones.ClearBoundedContext(id); err != nil {
		return err
	}
	return s.boundedContexts.Delete(id)
}

// AssignZoneToBoundedContext links a zone to a bounded context of the same project.
func (s *Service) AssignZoneToBoundedContext(zoneID, boundedContextID string) (*domain.Zone, error) {
	if s.boundedContexts == nil {
		return nil, errNoBoundedContexts
	}
	bc := s.boundedContexts.Get(boundedContextID)
	if bc == nil {
		return nil, &domain.StructuredError{Code: "BOUNDED_CONTEXT_NOT_FOUND", Message: "bounded context not found"}
	}
	zone := s.zones.Get(zoneID)
	if zone == nil {
		return nil, &domain.StructuredError{Code: "ZONE_NOT_FOUND", Message: "zone not found"}
	}
	if zone.ProjectID != bc.ProjectID {
		return nil, &domain.StructuredError{Code: "CROSS_PROJECT_ACCESS", Message: "zone and bounded context belong to different projects"}
	}
	return s.zones.SetBoundedContext(zoneID, boundedContextID)
}

// UnassignZoneFromBoundedContext clears a zone's bounded context link.
func (s *Service) UnassignZoneFromBoundedContext(zoneID string) (*domain.Zone, error) {
	return s.zones.SetBoundedContext(zoneID, "")
}
