// Package architecture is the architecture bounded context: how a project's
// code is carved into zones and bounded contexts, and its file tree.
//
// Zones reference prompts (rules) and agents from the agents context, and
// belong to a project from the projects context. They read those through
// ports and drop a reference when its owner announces it is gone.
package architecture

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.Architecture = (*Service)(nil)
	_ ports.ZoneReader   = (*Service)(nil)
)

// Service implements the architecture use cases.
type Service struct {
	zones           ports.ZoneRepository
	boundedContexts ports.BoundedContextRepository // nil: bounded contexts unavailable
	pathMatcher     ports.PathMatcher
	treeLister      ports.TreeLister
	defaultRoot     string

	projects ports.ProjectReader
	prompts  ports.PromptReader // nil: zone rules are returned as stored
}

// Deps are the Service's collaborators.
type Deps struct {
	Zones           ports.ZoneRepository
	BoundedContexts ports.BoundedContextRepository
	PathMatcher     ports.PathMatcher
	TreeLister      ports.TreeLister
	// DefaultRoot is the tree root when neither a root nor a project is given.
	DefaultRoot string
	Projects    ports.ProjectReader
	Prompts     ports.PromptReader
}

// NewService returns the architecture context.
func NewService(d Deps) *Service {
	return &Service{
		zones:           d.Zones,
		boundedContexts: d.BoundedContexts,
		pathMatcher:     d.PathMatcher,
		treeLister:      d.TreeLister,
		defaultRoot:     d.DefaultRoot,
		projects:        d.Projects,
		prompts:         d.Prompts,
	}
}

// Subscribe registers the context's reactions to other contexts' events.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.ProjectDeleted) error {
		if err := s.zones.DeleteByProject(ev.ProjectID); err != nil {
			return err
		}
		if s.boundedContexts == nil {
			return nil
		}
		return s.boundedContexts.DeleteByProject(ev.ProjectID)
	})
	ports.On(sub, func(_ context.Context, ev domain.PromptDeleted) error {
		return s.eachZone(func(z *domain.Zone) error {
			rules := make([]domain.Prompt, 0, len(z.Rules))
			for _, r := range z.Rules {
				if r.ID != ev.PromptID {
					rules = append(rules, r)
				}
			}
			if len(rules) == len(z.Rules) {
				return nil
			}
			_, err := s.zones.Update(z.ID, z.Name, z.Pattern, z.Purpose, rules, z.AssignedAgents)
			return err
		})
	})
	ports.On(sub, func(_ context.Context, ev domain.AgentDeleted) error {
		return s.eachZone(func(z *domain.Zone) error {
			agents := make([]domain.Agent, 0, len(z.AssignedAgents))
			for _, a := range z.AssignedAgents {
				if a.ID != ev.AgentID {
					agents = append(agents, a)
				}
			}
			if len(agents) == len(z.AssignedAgents) {
				return nil
			}
			_, err := s.zones.Update(z.ID, z.Name, z.Pattern, z.Purpose, z.Rules, agents)
			return err
		})
	})
}

// eachZone runs f over every zone of every project, stopping at the first error.
func (s *Service) eachZone(f func(z *domain.Zone) error) error {
	if s.projects == nil {
		return nil
	}
	projects, err := s.projects.ListProjects(context.TODO())
	if err != nil {
		return err
	}
	for _, p := range projects {
		for _, z := range s.zones.ListByProject(p.ID) {
			if err := f(z); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveRoot returns the root path for tree/path operations. If root is non-empty it is used;
// else if projectID is non-empty the project's RootDir is used; otherwise DefaultRoot.
func (s *Service) resolveRoot(root, projectID string) (string, error) {
	if root != "" {
		return root, nil
	}
	if projectID != "" {
		if s.projects == nil {
			return "", &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
		}
		p, err := s.projects.GetProject(context.TODO(), projectID)
		if err != nil {
			return "", err
		}
		return p.RootDir, nil
	}
	return s.defaultRoot, nil
}

// ListMatchingPaths returns paths under root that match the regex pattern.
// root and projectID are optional; if both empty, DefaultRoot is used.
func (s *Service) ListMatchingPaths(root, projectID, pattern string) ([]string, error) {
	r, err := s.resolveRoot(root, projectID)
	if err != nil {
		return nil, err
	}
	return s.pathMatcher.ListMatchingPaths(r, pattern)
}

// ListTree returns the directory tree from root.
// root and projectID are optional; if both empty, DefaultRoot is used.
func (s *Service) ListTree(root, projectID string) (*domain.TreeNode, error) {
	r, err := s.resolveRoot(root, projectID)
	if err != nil {
		return nil, err
	}
	return s.treeLister.ListTree(r)
}
