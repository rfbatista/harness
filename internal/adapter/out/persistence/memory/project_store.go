package memory

import (
	"sync"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Ensure ProjectStore implements ports.ProjectRepository at compile time.
var _ ports.ProjectRepository = (*ProjectStore)(nil)

// ProjectStore holds in-memory projects keyed by id.
type ProjectStore struct {
	mu       sync.RWMutex
	projects map[string]*domain.Project
}

// NewProjectStore returns a new in-memory project store.
func NewProjectStore() *ProjectStore {
	return &ProjectStore{projects: make(map[string]*domain.Project)}
}

// Get returns the project by id, or nil if not found.
func (s *ProjectStore) Get(id string) *domain.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[id]
	if !ok {
		return nil
	}
	return cloneProject(p)
}

// List returns all projects.
func (s *ProjectStore) List() []*domain.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, cloneProject(p))
	}
	return out
}

// Create creates a project with generated id. RootDir is required (can be absolute or relative).
func (s *ProjectStore) Create(name, rootDir string) (*domain.Project, error) {
	if rootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "project root directory is required"}
	}
	id, err := genID()
	if err != nil {
		return nil, err
	}
	p := &domain.Project{
		ID:           id,
		Name:         name,
		RootDir:      rootDir,
		IgnoredPaths: []string{},
	}
	s.mu.Lock()
	s.projects[id] = p
	s.mu.Unlock()
	return cloneProject(p), nil
}

// Update updates a project by id.
func (s *ProjectStore) Update(id, name, rootDir string) (*domain.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	if name != "" {
		p.Name = name
	}
	if rootDir != "" {
		p.RootDir = rootDir
	}
	return cloneProject(p), nil
}

// Delete removes a project by id. Returns PROJECT_NOT_FOUND if it does not exist.
func (s *ProjectStore) Delete(projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[projectID]; !ok {
		return &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	delete(s.projects, projectID)
	return nil
}

func cloneProject(p *domain.Project) *domain.Project {
	if p == nil {
		return nil
	}
	c := *p
	c.IgnoredPaths = append([]string(nil), p.IgnoredPaths...)
	return &c
}

// AddIgnoredPath adds path to the project's ignored list (no-op if already present).
func (s *ProjectStore) AddIgnoredPath(projectID, path string) (*domain.Project, error) {
	path = domain.NormalizePath(path)
	if path == "" {
		return nil, &domain.StructuredError{Code: "INVALID_PATH", Message: "path is required"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[projectID]
	if !ok {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	for _, ig := range p.IgnoredPaths {
		if ig == path {
			return cloneProject(p), nil
		}
	}
	p.IgnoredPaths = append(p.IgnoredPaths, path)
	return cloneProject(p), nil
}

// RemoveIgnoredPath removes path from the project's ignored list.
func (s *ProjectStore) RemoveIgnoredPath(projectID, path string) (*domain.Project, error) {
	path = domain.NormalizePath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[projectID]
	if !ok {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	filtered := p.IgnoredPaths[:0]
	for _, ig := range p.IgnoredPaths {
		if ig != path {
			filtered = append(filtered, ig)
		}
	}
	p.IgnoredPaths = filtered
	return cloneProject(p), nil
}
