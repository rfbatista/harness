// Package designsystem is the design system context: one structured record
// per project (ADR 0054) — tokens, principles and components — saved only by
// the person here and kept as immutable versions.
//
// The latest version is the design system; an older one is read, never
// restored.
package designsystem

import (
	"context"
	"fmt"
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
	now      func() time.Time
}

// NewService returns the design system context.
func NewService(d Deps) *Service {
	return &Service{store: d.Store, projects: d.Projects, now: time.Now}
}

// UseFeed sets where saves are announced: the project feed, which the
// orchestration owns.
func (s *Service) UseFeed(feed ports.ProjectChangeSink) { s.feed = feed }

// Subscribe registers the context's reactions to other contexts' events: a
// project's design system goes with it.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.ProjectDeleted) error {
		return s.store.DeleteByProject(ctx, ev.ProjectID)
	})
}

func (s *Service) announce(v *domain.DesignSystemVersion) {
	if s.feed == nil {
		return
	}
	s.feed.AnnounceChange(ports.ProjectChange{DesignSystemSaved: &ports.DesignSystemChange{
		ProjectID: v.ProjectID, Version: v.Version, Source: v.Source,
	}})
}

// current is the project's newest version, nil when it has none.
func (s *Service) current(ctx context.Context, projectID string) (*domain.DesignSystemVersion, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.store.Latest(ctx, projectID)
}

// GetDesignSystem implements ports.DesignSystems.
func (s *Service) GetDesignSystem(ctx context.Context, projectID string) (*domain.DesignSystem, error) {
	cur, err := s.current(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ds := &domain.DesignSystem{ProjectID: projectID}
	if cur == nil {
		ds.Record.Normalize()
		return ds, nil
	}
	ds.Exists, ds.Version, ds.UpdatedAt, ds.UpdatedBy, ds.Record = true, cur.Version, cur.At, cur.By, cur.Record
	return ds, nil
}

// SaveDesignSystem implements ports.DesignSystems.
func (s *Service) SaveDesignSystem(ctx context.Context, projectID string, baseVersion int, record domain.DesignSystemRecord, message string) (*domain.DesignSystemVersion, error) {
	cur, err := s.current(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.write(ctx, projectID, cur, baseVersion, record, message)
}

// RenameDesignToken implements ports.DesignSystems.
func (s *Service) RenameDesignToken(ctx context.Context, projectID string, baseVersion int, group, name, newGroup, newName string) (*domain.DesignSystemVersion, error) {
	cur, err := s.current(ctx, projectID)
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
	return s.write(ctx, projectID, cur, baseVersion, record, message)
}

// write validates record against the current version and appends it as the
// next one, then announces it.
func (s *Service) write(ctx context.Context, projectID string, cur *domain.DesignSystemVersion, baseVersion int, record domain.DesignSystemRecord, message string) (*domain.DesignSystemVersion, error) {
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
		ProjectID: projectID, At: s.now(), By: domain.DesignByPerson, Source: domain.DesignSourceManual,
		Message: message, Summary: domain.SummarizeDesignChange(previous, record), Record: record,
	}
	if err := s.store.Append(ctx, v, baseVersion); err != nil {
		return nil, err
	}
	s.announce(v)
	return v, nil
}

// ListDesignSystemVersions implements ports.DesignSystems.
func (s *Service) ListDesignSystemVersions(ctx context.Context, projectID string, limit, before int) ([]*domain.DesignSystemVersion, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultVersionsLimit
	}
	return s.store.List(ctx, projectID, min(limit, maxVersionsLimit), before)
}

// GetDesignSystemVersion implements ports.DesignSystems.
func (s *Service) GetDesignSystemVersion(ctx context.Context, projectID string, version int) (*domain.DesignSystemVersion, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	v, err := s.store.Get(ctx, projectID, version)
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
