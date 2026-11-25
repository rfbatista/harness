// Package productowner is a project's product owner (ADR 0109): its settings
// and the threads in which the person talks to it. A thread is a headless
// session in product-owner mode; this package keeps what the person reads of
// it (the thread's items) apart from the claude transcript.
package productowner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
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
	sessions ports.ProductOwnerSessions

	// idle is how long a thread's process waits for the next message before
	// it ends.
	idle time.Duration

	mu      sync.Mutex
	runs    map[string]*run // by thread id: the threads whose process this server follows
	locks   map[string]*sync.Mutex
	closing bool
	// rowMu serialises the writes of a thread row; recovering holds the threads
	// the boot is resuming.
	rowMu      sync.Mutex
	recovering map[string]bool
	// waking holds the threads whose session a message is waking up.
	waking map[string]bool
	// digestTimeChanged is told when a project's digest time changes (ADR 0113).
	digestTimeChanged func(ctx context.Context, projectID, before, after string)
	// digests are the project digests the web UI's API reads through here.
	digests *Digests
	// memMu serialises the writes of memory entries.
	memMu sync.Mutex
	// held is what the held actions share (held.go).
	held heldState
}

// OnDigestTimeChanged sets who is told when a project's digest time changes.
func (s *Service) OnDigestTimeChanged(fn func(ctx context.Context, projectID, before, after string)) {
	s.digestTimeChanged = fn
}

// DefaultIdle is how long a thread's process stays after its last turn.
const DefaultIdle = 10 * time.Minute

// New returns the product owner service. feed puts each change on its
// project's feed, where the page stream follows it; nil announces nothing.
// sessions starts and drives the threads' sessions; nil leaves threads
// unavailable.
func New(store ports.ProductOwnerStore, projects Projects, agents Agents, feed ports.ProjectChangeSink, sessions ports.ProductOwnerSessions) *Service {
	return &Service{store: store, projects: projects, agents: agents, feed: feed, sessions: sessions, idle: DefaultIdle,
		runs: map[string]*run{}, locks: map[string]*sync.Mutex{}, recovering: map[string]bool{}, waking: map[string]bool{}}
}

// SetIdle changes how long an idle thread's process stays; it applies to the
// processes that start after.
func (s *Service) SetIdle(d time.Duration) {
	s.mu.Lock()
	s.idle = d
	s.mu.Unlock()
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
	v := &ports.ProductOwnerSettingsView{ProjectID: projectID, Enabled: st.Enabled, Prompt: st.Prompt, TimeZone: serverZoneName(time.Now())}
	if st.DigestTime != "" {
		t := st.DigestTime
		v.DigestTime = &t
		if st.Enabled && s.digests != nil {
			if next, ok, err := s.digests.NextAt(ctx, projectID, st.DigestTime); err == nil && ok {
				v.NextDigestAt = &next
			}
		}
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
	digestBefore := st.DigestTime
	if u.DigestTime != nil {
		if err := domain.ValidDigestTime(*u.DigestTime); err != nil {
			return nil, err
		}
		st.DigestTime = *u.DigestTime
	}
	if err := s.store.SaveSettings(ctx, st); err != nil {
		return nil, err
	}
	if s.digestTimeChanged != nil && st.DigestTime != digestBefore {
		s.digestTimeChanged(ctx, u.ProjectID, digestBefore, st.DigestTime)
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

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
