package repoarch

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"

	"operators-mcp/internal/domain"
)

// defaultBranchFiles is the files of the repository's default branch in its
// local checkout, less the repository's ignored paths, sorted.
func (s *Service) defaultBranchFiles(ctx context.Context, repo *domain.Repository) (branch string, files []string, err error) {
	if s.git == nil {
		return "", nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the checkout cannot be read"}
	}
	if branch, err = s.git.DefaultBranch(ctx, repo.RootDir); err != nil {
		return "", nil, err
	}
	if branch == "" {
		return "", nil, nil
	}
	all, err := s.git.Files(ctx, repo.RootDir, branch)
	if err != nil {
		return "", nil, err
	}
	for _, f := range all {
		if !ignored(repo.IgnoredPaths, f) {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return branch, files, nil
}

func ignored(paths []string, file string) bool {
	for _, p := range paths {
		if p = strings.Trim(p, "/"); p != "" && (file == p || strings.HasPrefix(file, p+"/")) {
			return true
		}
	}
	return false
}

// PreviewLayers implements ports.RepositoryArchitectures.
func (s *Service) PreviewLayers(ctx context.Context, repositoryID string, layers []domain.ArchLayer) (*domain.LayersPreview, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	_, files, err := s.defaultBranchFiles(ctx, repo)
	if err != nil {
		return nil, err
	}
	for i := range layers {
		layers[i].Match.Folder = strings.TrimSuffix(layers[i].Match.Folder, "/")
	}
	p := domain.PreviewLayers(layers, files)
	return &p, nil
}

// DiscoverRepositoryArchitecture implements ports.RepositoryArchitectures.
func (s *Service) DiscoverRepositoryArchitecture(ctx context.Context, repositoryID string) (*domain.ArchitectureDiscovery, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	branch, files, err := s.defaultBranchFiles(ctx, repo)
	if err != nil {
		return nil, err
	}
	d := &domain.ArchitectureDiscovery{
		Instructions: []domain.ArchitectureCandidate{}, Tools: []domain.ArchitectureCandidate{},
		Sandboxes: []domain.ArchitectureCandidate{}, Agents: []string{},
	}
	hasMCPJSON := false
	for _, f := range files {
		switch base := path.Base(f); {
		case base == "AGENTS.md" || base == "CLAUDE.md" || base == "GEMINI.md":
			d.Instructions = append(d.Instructions, domain.ArchitectureCandidate{Path: f, Kind: "rules", Loaded: "always", Source: "repository"})
		case base == "SKILL.md" && strings.HasPrefix(f, ".claude/skills/"):
			d.Instructions = append(d.Instructions, domain.ArchitectureCandidate{Path: f, Kind: "skill", Loaded: "on_demand", Source: "repository"})
		case strings.HasPrefix(f, ".claude/agents/") && path.Dir(f) == ".claude/agents" && strings.HasSuffix(f, ".md"):
			d.Instructions = append(d.Instructions, domain.ArchitectureCandidate{Path: f, Kind: "subagent_prompt", Loaded: "on_demand", Source: "repository"})
		case f == ".mcp.json":
			hasMCPJSON = true
		}
	}

	seen := map[string]bool{}
	if hasMCPJSON {
		if raw, ok, err := s.git.ReadFile(ctx, repo.RootDir, branch, ".mcp.json"); err == nil && ok {
			var cfg struct {
				Servers map[string]json.RawMessage `json:"mcpServers"`
			}
			if json.Unmarshal(raw, &cfg) == nil {
				names := make([]string, 0, len(cfg.Servers))
				for n := range cfg.Servers {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					seen[n] = true
					d.Tools = append(d.Tools, domain.ArchitectureCandidate{Name: n, Kind: "mcp", Source: ".mcp.json"})
				}
			}
		}
	}
	if s.mcp != nil {
		servers := s.mcp.ListMCPServers()
		sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
		for _, m := range servers {
			if !seen[m.Name] {
				seen[m.Name] = true
				d.Tools = append(d.Tools, domain.ArchitectureCandidate{Name: m.Name, Kind: "mcp", Source: "mcp catalog"})
			}
		}
	}

	if s.sandboxDriver != nil {
		if drv := s.sandboxDriver(); drv != nil {
			c := domain.ArchitectureCandidate{Name: drv.Name(), Source: "sandbox settings"}
			if s.sandboxSettings != nil {
				for _, p := range s.sandboxSettings.Paths() {
					c.CanReach = append(c.CanReach, p.Path)
				}
			}
			d.Sandboxes = append(d.Sandboxes, c)
		}
	}

	agents, err := s.agents.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		d.Agents = append(d.Agents, a.Name)
		if a.PromptID != "" {
			d.Instructions = append(d.Instructions, domain.ArchitectureCandidate{
				Path: "harness://agents/" + a.Name, Kind: "subagent_prompt", Loaded: "on_demand", Source: "agent catalog",
			})
		}
	}
	sort.Strings(d.Agents)
	return d, nil
}
