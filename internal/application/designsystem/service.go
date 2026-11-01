// Package designsystem is the design system context: a project holds many
// named design systems (ADR 0057), each one structured record — tokens,
// principles and components — saved only by the person here and kept as
// immutable versions.
//
// The latest version is the design system; an older one is read, never
// restored.
package designsystem

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.DesignSystems = (*Service)(nil)

const (
	defaultVersionsLimit = 50
	maxVersionsLimit     = 200
)

// Deps are the Service's collaborators.
type Deps struct {
	Store    ports.DesignSystemStore
	Projects ports.ProjectReader
}

// Service implements the design system use cases.
type Service struct {
	store    ports.DesignSystemStore
	projects ports.ProjectReader
	feed     ports.ProjectChangeSink // nil until the orchestration is wired
	// guards refuse a delete (DESIGN_SYSTEM_IN_USE).
	guards []func(ctx context.Context, designSystemID string) error
	lock   sync.Locker // held from the guards to the delete; nil: none
	now    func() time.Time
}

// NewService returns the design system context.
func NewService(d Deps) *Service {
	return &Service{store: d.Store, projects: d.Projects, now: time.Now}
}

// UseFeed sets where changes are announced: the project feed, which the
// orchestration owns.
func (s *Service) UseFeed(feed ports.ProjectChangeSink) { s.feed = feed }

// UseLock sets the lock a delete holds from its guards to the store's delete;
// whoever lets something start using a design system takes the same lock.
func (s *Service) UseLock(l sync.Locker) { s.lock = l }

// GuardDelete adds a check a delete passes first; an error refuses it.
func (s *Service) GuardDelete(check func(ctx context.Context, designSystemID string) error) {
	s.guards = append(s.guards, check)
}

// Subscribe registers the context's reactions to other contexts' events: a
// project's design systems go with it.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.ProjectDeleted) error {
		return s.store.DeleteByProject(ctx, ev.ProjectID)
	})
}

func (s *Service) announceSaved(projectID string, v *domain.DesignSystemVersion) {
	if s.feed == nil {
		return
	}
	s.feed.AnnounceChange(ports.ProjectChange{DesignSystemSaved: &ports.DesignSystemChange{
		ProjectID: projectID, DesignSystemID: v.DesignSystemID, Version: v.Version, Source: v.Source,
	}})
}

func (s *Service) announceChanged(projectID, designSystemID string) {
	if s.feed == nil {
		return
	}
	s.feed.AnnounceChange(ports.ProjectChange{DesignSystemChanged: &ports.DesignSystemIdentityChange{
		ProjectID: projectID, DesignSystemID: designSystemID,
	}})
}

// identity is the design system's identity, DESIGN_SYSTEM_NOT_FOUND when
// there is none.
func (s *Service) identity(ctx context.Context, designSystemID string) (*domain.DesignSystemIdentity, error) {
	id, err := s.store.Identity(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, &domain.StructuredError{Code: "DESIGN_SYSTEM_NOT_FOUND", Message: "design system not found"}
	}
	return id, nil
}

// current is the design system's identity and its newest version, nil when it
// has none.
func (s *Service) current(ctx context.Context, designSystemID string) (*domain.DesignSystemIdentity, *domain.DesignSystemVersion, error) {
	id, err := s.identity(ctx, designSystemID)
	if err != nil {
		return nil, nil, err
	}
	cur, err := s.store.Latest(ctx, designSystemID)
	return id, cur, err
}

// view is the design system as it is now.
func view(id *domain.DesignSystemIdentity, cur *domain.DesignSystemVersion) *domain.DesignSystem {
	ds := &domain.DesignSystem{ID: id.ID, ProjectID: id.ProjectID, Name: id.Name, Slug: id.Slug, Description: id.Description}
	if cur == nil {
		ds.Record.Normalize()
		return ds
	}
	ds.Exists, ds.Version, ds.UpdatedAt, ds.UpdatedBy, ds.Record = true, cur.Version, cur.At, cur.By, cur.Record
	return ds
}

// ListDesignSystems implements ports.DesignSystems.
func (s *Service) ListDesignSystems(ctx context.Context, projectID string) ([]*domain.DesignSystemSummary, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	ids, err := s.store.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	stamps, err := s.store.LatestStamps(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.DesignSystemSummary, 0, len(ids))
	for _, id := range ids {
		sum := &domain.DesignSystemSummary{DesignSystemIdentity: *id}
		if st, ok := stamps[id.ID]; ok {
			sum.Version = st.Version
			if st.At.After(sum.UpdatedAt) {
				sum.UpdatedAt = st.At
			}
		}
		out = append(out, sum)
	}
	slices.SortStableFunc(out, func(a, b *domain.DesignSystemSummary) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return out, nil
}

// invalidIdentity is the refusal of a name or slug.
func invalidIdentity(problems []domain.DesignFieldError) error {
	if len(problems) == 0 {
		return nil
	}
	return invalid(problems)
}

// CreateDesignSystem implements ports.DesignSystems.
func (s *Service) CreateDesignSystem(ctx context.Context, projectID, name, slug, description string) (*domain.DesignSystemIdentity, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	problems := append(domain.ValidateDesignSystemName(name), domain.ValidateDesignSystemSlug(slug)...)
	if err := invalidIdentity(problems); err != nil {
		return nil, err
	}
	id := &domain.DesignSystemIdentity{ProjectID: projectID, Name: name, Slug: slug, Description: strings.TrimSpace(description)}
	if err := s.store.Create(ctx, id); err != nil {
		return nil, err
	}
	s.announceChanged(projectID, id.ID)
	return id, nil
}

// UpdateDesignSystem implements ports.DesignSystems.
func (s *Service) UpdateDesignSystem(ctx context.Context, designSystemID string, name, description *string) (*domain.DesignSystem, error) {
	id, cur, err := s.current(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		id.Name = strings.TrimSpace(*name)
		if err := invalidIdentity(domain.ValidateDesignSystemName(id.Name)); err != nil {
			return nil, err
		}
	}
	if description != nil {
		id.Description = strings.TrimSpace(*description)
	}
	if err := s.store.Update(ctx, id); err != nil {
		return nil, err
	}
	s.announceChanged(id.ProjectID, id.ID)
	return view(id, cur), nil
}

// DeleteDesignSystem implements ports.DesignSystems.
func (s *Service) DeleteDesignSystem(ctx context.Context, designSystemID, confirmName string) error {
	id, err := s.identity(ctx, designSystemID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirmName) != id.Name {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "confirm_name must be the design system's name, " + id.Name}
	}
	if s.lock != nil {
		s.lock.Lock()
		defer s.lock.Unlock()
	}
	for _, guard := range s.guards {
		if err := guard(ctx, designSystemID); err != nil {
			return err
		}
	}
	if err := s.store.Delete(ctx, designSystemID); err != nil {
		return err
	}
	s.announceChanged(id.ProjectID, id.ID)
	return nil
}

// GetDesignSystem implements ports.DesignSystems.
func (s *Service) GetDesignSystem(ctx context.Context, designSystemID string) (*domain.DesignSystem, error) {
	id, cur, err := s.current(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	return view(id, cur), nil
}

// SaveDesignSystem implements ports.DesignSystems.
func (s *Service) SaveDesignSystem(ctx context.Context, designSystemID string, baseVersion int, record domain.DesignSystemRecord, message string) (*domain.DesignSystemVersion, error) {
	id, cur, err := s.current(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	return s.write(ctx, id, cur, baseVersion, record, message)
}

// RenameDesignToken implements ports.DesignSystems.
func (s *Service) RenameDesignToken(ctx context.Context, designSystemID string, baseVersion int, group, name, newGroup, newName string) (*domain.DesignSystemVersion, error) {
	id, cur, err := s.current(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	if err := checkBase(cur, baseVersion); err != nil {
		return nil, err
	}
	var previous domain.DesignSystemRecord
	if cur != nil {
		previous = cur.Record
	}
	record, ok := previous.RenameToken(group, name, newGroup, newName)
	if !ok {
		return nil, invalid([]domain.DesignFieldError{{
			Path: "/tokens", Code: domain.DesignUnknownToken, Message: fmt.Sprintf("there is no token %s.%s", group, name),
		}})
	}
	message := fmt.Sprintf("Renamed %s.%s to %s.%s", group, name, newGroup, newName)
	return s.write(ctx, id, cur, baseVersion, record, message)
}

// write validates record against the current version and appends it as the
// next one, then announces it.
func (s *Service) write(ctx context.Context, id *domain.DesignSystemIdentity, cur *domain.DesignSystemVersion, baseVersion int, record domain.DesignSystemRecord, message string) (*domain.DesignSystemVersion, error) {
	if err := checkBase(cur, baseVersion); err != nil {
		return nil, err
	}
	var previous domain.DesignSystemRecord
	if cur != nil {
		previous = cur.Record
	}
	record.Normalize()
	if problems := record.Validate(previous); len(problems) > 0 {
		return nil, invalid(problems)
	}
	v := &domain.DesignSystemVersion{
		DesignSystemID: id.ID, At: s.now(), By: domain.DesignByPerson, Source: domain.DesignSourceManual,
		Message: message, Summary: domain.SummarizeDesignChange(previous, record), Record: record,
	}
	if err := s.store.Append(ctx, v, baseVersion); err != nil {
		return nil, err
	}
	s.announceSaved(id.ProjectID, v)
	return v, nil
}

// ListDesignSystemVersions implements ports.DesignSystems.
func (s *Service) ListDesignSystemVersions(ctx context.Context, designSystemID string, limit, before int) ([]*domain.DesignSystemVersion, error) {
	if _, err := s.identity(ctx, designSystemID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultVersionsLimit
	}
	return s.store.List(ctx, designSystemID, min(limit, maxVersionsLimit), before)
}

// GetDesignSystemVersion implements ports.DesignSystems.
func (s *Service) GetDesignSystemVersion(ctx context.Context, designSystemID string, version int) (*domain.DesignSystemVersion, error) {
	if _, err := s.identity(ctx, designSystemID); err != nil {
		return nil, err
	}
	v, err := s.store.Get(ctx, designSystemID, version)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, &domain.StructuredError{Code: "VERSION_NOT_FOUND", Message: fmt.Sprintf("the design system has no version %d", version)}
	}
	return v, nil
}

// checkBase refuses a base that is not the current version (0 when there is
// none).
func checkBase(cur *domain.DesignSystemVersion, baseVersion int) error {
	current := 0
	if cur != nil {
		current = cur.Version
	}
	if current == baseVersion {
		return nil
	}
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "DESIGN_SYSTEM_CHANGED", Message: "the design system was saved since you loaded it"},
		Details:         map[string]any{"current_version": current},
	}
}

func invalid(problems []domain.DesignFieldError) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "DESIGN_SYSTEM_INVALID", Message: "the design system is invalid"},
		Details:         map[string]any{"errors": problems},
	}
}
