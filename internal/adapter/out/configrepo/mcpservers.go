package configrepo

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rfbatista/harnesskit/mcpserver"
)

// MCPServerStore implements mcpserver.Store (ports.MCPServerRepository) by
// parsing every distinct .mcp.json referenced by the manifest's agents, live,
// on every call. Secret-bearing fields (env values, header values) are
// whatever the .mcp.json file itself contains — including unexpanded ${VAR}
// placeholders — never resolved here. Create/Update/Delete always fail: the
// files are the source of truth, not a store this process writes to.
type MCPServerStore struct {
	ManifestPath string
	AgentsRoot   string
}

// NewMCPServerStore returns an MCPServerStore reading the manifest at
// manifestPath fresh on every call, whose agents' "mcp"/"dir" fields resolve
// to .mcp.json paths under agentsRoot.
func NewMCPServerStore(manifestPath, agentsRoot string) *MCPServerStore {
	return &MCPServerStore{ManifestPath: manifestPath, AgentsRoot: agentsRoot}
}

// List re-parses every referenced .mcp.json, deduped by server name (first
// file that defines a given name wins), in name order.
func (s *MCPServerStore) List() []*mcpserver.Server {
	byName := map[string]*mcpserver.Server{}
	var order []string
	for _, path := range s.mcpPaths() {
		for _, srv := range s.serversAt(path) {
			srv := srv
			if _, exists := byName[srv.Name]; exists {
				continue
			}
			srv.ID = IDPrefix + srv.Name
			byName[srv.Name] = &srv
			order = append(order, srv.Name)
		}
	}
	out := make([]*mcpserver.Server, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get re-reads one server by its "cfg:"-prefixed id.
func (s *MCPServerStore) Get(id string) *mcpserver.Server {
	name, ok := stripIDPrefix(id)
	if !ok {
		return nil
	}
	for _, srv := range s.List() {
		if srv.Name == name {
			return srv
		}
	}
	return nil
}

// resolvePath resolves one agent's mcp/dir config to a .mcp.json path
// (mirrors resolve_agent() in agents/agent_launch.py), or "" if the agent has
// no "mcp" set or the file doesn't exist on disk.
func (s *MCPServerStore) resolvePath(key string, cfg AgentConfig) string {
	if cfg.MCP == "" {
		return ""
	}
	ownDir := cfg.Dir
	if ownDir == "" {
		ownDir = key
	}
	mcpDir := ownDir
	if cfg.MCP != "self" {
		mcpDir = cfg.MCP
	}
	path := filepath.Join(s.AgentsRoot, mcpDir, ".mcp.json")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// mcpPaths re-reads the manifest and returns every distinct resolved
// .mcp.json path across all agents.
func (s *MCPServerStore) mcpPaths() []string {
	manifest, err := LoadManifest(s.ManifestPath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var paths []string
	for key, cfg := range manifest.Agents {
		path := s.resolvePath(key, cfg)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// serversAt parses the .mcp.json at path, or nil if it can't be read/parsed.
func (s *MCPServerStore) serversAt(path string) []mcpserver.Server {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	parsed, err := mcpserver.ParseCursorConfig(string(content))
	if err != nil {
		return nil
	}
	return parsed
}

// serverNamesFor returns the MCP server names one agent's config resolves
// to, for AgentRepository to build MCPServerIDs without re-implementing path
// resolution or parsing itself.
func (s *MCPServerStore) serverNamesFor(key string, cfg AgentConfig) []string {
	path := s.resolvePath(key, cfg)
	if path == "" {
		return nil
	}
	servers := s.serversAt(path)
	names := make([]string, len(servers))
	for i, srv := range servers {
		names[i] = srv.Name
	}
	return names
}

func (s *MCPServerStore) Create(mcpserver.Input) (*mcpserver.Server, error) {
	return nil, errReadOnly("mcp server")
}

func (s *MCPServerStore) Update(string, mcpserver.Input) (*mcpserver.Server, error) {
	return nil, errReadOnly("mcp server")
}

func (s *MCPServerStore) UpdateProbeResult(string, mcpserver.ProbeResult) (*mcpserver.Server, error) {
	return nil, errReadOnly("mcp server")
}

func (s *MCPServerStore) Delete(string) error { return errReadOnly("mcp server") }
