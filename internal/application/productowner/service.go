// Package productowner is a project's product owner (ADR 0109): its settings
// and the threads in which the person talks to it. A thread is a headless
// session in product-owner mode; this package keeps what the person reads of
// it (the thread's items) apart from the claude transcript.
package productowner

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Projects is what the product owner reads of the project catalog.
type Projects interface {
	GetProject(ctx context.Context, projectID string) (*domain.Project, error)
}

// Agents is what it reads of the agent catalog.
type Agents interface {
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
}

// Service is the product owner of every project.
type Service struct {
	store    ports.ProductOwnerStore
	projects Projects
	agents   Agents
	feed     ports.ProjectChangeSink
}

// New returns the product owner service. feed puts each change on its
// project's feed, where the page stream follows it; nil announces nothing.
func New(store ports.ProductOwnerStore, projects Projects, agents Agents, feed ports.ProjectChangeSink) *Service {
	return &Service{store: store, projects: projects, agents: agents, feed: feed}
}

var _ ports.ProductOwner = (*Service)(nil)

func invalidInput(msg string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg}
}

// Settings implements ports.ProductOwner.
func (s *Service) Settings(ctx context.Context, projectID string) (*ports.ProductOwnerSettingsView, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.view(ctx, projectID)
}

func (s *Service) view(ctx context.Context, projectID string) (*ports.ProductOwnerSettingsView, error) {
	st, err := s.store.Settings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	zone, _ := time.Now().Zone()
	if loc := time.Local.String(); loc != "Local" {
		zone = loc
	}
	v := &ports.ProductOwnerSettingsView{ProjectID: projectID, Enabled: st.Enabled, Prompt: st.Prompt, TimeZone: zone}
	if st.DigestTime != "" {
		t := st.DigestTime
		v.DigestTime = &t
	}
	if st.AgentID != "" {
		// An agent deleted since it was chosen reads as none: the product owner
		// runs as plain claude.
		if a, err := s.agents.GetAgent(ctx, st.AgentID); err == nil && a != nil {
			v.Agent = &ports.ProductOwnerAgent{ID: a.ID, Name: a.Name}
		}
	}
	return v, nil
}

// UpdateSettings implements ports.ProductOwner. A refused update changes
// nothing.
func (s *Service) UpdateSettings(ctx context.Context, u ports.ProductOwnerSettingsUpdate) (*ports.ProductOwnerSettingsView, error) {
	if _, err := s.projects.GetProject(ctx, u.ProjectID); err != nil {
		return nil, err
	}
	st, err := s.store.Settings(ctx, u.ProjectID)
	if err != nil {
		return nil, err
	}
	if u.Enabled != nil {
		st.Enabled = *u.Enabled
	}
	if u.AgentID != nil {
		if *u.AgentID != "" {
			if _, err := s.agents.GetAgent(ctx, *u.AgentID); err != nil {
				return nil, err
			}
		}
		st.AgentID = *u.AgentID
	}
	if u.Prompt != nil {
		if len([]rune(*u.Prompt)) > domain.ProductOwnerMaxPrompt {
			return nil, invalidInput("prompt is over 20,000 characters")
		}
		st.Prompt = *u.Prompt
	}
	if u.DigestTime != nil {
		if err := domain.ValidDigestTime(*u.DigestTime); err != nil {
			return nil, err
		}
		st.DigestTime = *u.DigestTime
	}
	if err := s.store.SaveSettings(ctx, st); err != nil {
		return nil, err
	}
	v, err := s.view(ctx, u.ProjectID)
	if err != nil {
		return nil, err
	}
	s.announce(ports.ProjectChange{ProductOwnerSettings: &ports.ProductOwnerSettingsChange{ProjectID: u.ProjectID, Settings: v}})
	return v, nil
}

func (s *Service) announce(c ports.ProjectChange) {
	if s.feed != nil {
		s.feed.AnnounceChange(c)
	}
}
