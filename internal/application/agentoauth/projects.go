package agentoauth

import (
	"os"
	"path/filepath"
	"sync"

	"operators-mcp/internal/ports"
)

// ProjectsDeps are what the projects' flows run on: the global Deps, with the
// home of each project in place of the agent Claude home.
type ProjectsDeps struct {
	Deps
	// HomesDir is the folder that holds the project Claude homes, one folder
	// per project id. A project's home is created and seeded by Seed on the
	// project's first sign-in.
	HomesDir string
	// Seed makes the home at dir ready for claude. Nil: the folder only.
	Seed func(dir string) error
	// WrapFor confines the processes of one project's flow; nil runs them
	// unwrapped.
	WrapFor func(projectID string) Wrap
	// HasLoginIn says whether the home at dir holds a login. Nil: the login
	// file claude keeps, .credentials.json.
	HasLoginIn func(dir string) bool
	// OnDone runs when a project's flow ends signed in: the login replaces the
	// project's pasted credential. Nil: none.
	OnDone func(projectID string)
}

// Projects is the sign-in flow of each project: the global machinery pointed
// at the project Claude home (ADR 0102).
type Projects struct {
	d ProjectsDeps

	mu    sync.Mutex
	flows map[string]*Service
}

var _ ports.ProjectAgentOAuth = (*Projects)(nil)

// NewProjects builds the projects' flows.
func NewProjects(d ProjectsDeps) *Projects {
	if d.HasLoginIn == nil {
		d.HasLoginIn = func(dir string) bool {
			_, err := os.Stat(filepath.Join(dir, ".credentials.json"))
			return err == nil
		}
	}
	return &Projects{d: d, flows: map[string]*Service{}}
}

// Home is the project Claude home of projectID.
func (p *Projects) Home(projectID string) string {
	return filepath.Join(p.d.HomesDir, projectID)
}

// For is the project's flow, made on first use.
func (p *Projects) For(projectID string) ports.AgentSignIn {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.flows[projectID]; ok {
		return s
	}
	d := p.d.Deps
	d.ConfigDir = p.Home(projectID)
	if p.d.WrapFor != nil {
		d.Wrap = p.d.WrapFor(projectID)
	}
	if p.d.OnDone != nil {
		d.OnDone = func() { p.d.OnDone(projectID) }
	}
	d.Prepare = func() error {
		if p.d.Seed != nil {
			return p.d.Seed(d.ConfigDir)
		}
		return os.MkdirAll(d.ConfigDir, 0o700)
	}
	s := New(d)
	p.flows[projectID] = s
	return s
}

// HasLogin says whether the project Claude home holds a login.
func (p *Projects) HasLogin(projectID string) bool {
	return p.d.HasLoginIn(p.Home(projectID))
}

// Close ends every flow that runs and waits until its process is reaped.
func (p *Projects) Close() {
	p.mu.Lock()
	flows := make([]*Service, 0, len(p.flows))
	for _, s := range p.flows {
		flows = append(flows, s)
	}
	p.mu.Unlock()
	for _, s := range flows {
		s.Close()
	}
}
