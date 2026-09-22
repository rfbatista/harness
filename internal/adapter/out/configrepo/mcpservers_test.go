package configrepo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rfbatista/harnesskit/mcpserver"
)

const backendMCPJSON = `{
  "mcpServers": {
    "postgres": {"command": "uv", "args": ["run", "postgres-mcp"]},
    "github": {"type": "http", "url": "https://api.githubcopilot.com/mcp/",
               "headers": {"Authorization": "Bearer ${GITHUB_PERSONAL_ACCESS_TOKEN}"}}
  }
}`

func writeMCPJSON(t *testing.T, agentsRoot, ownerDir, content string) {
	t.Helper()
	dir := filepath.Join(agentsRoot, ownerDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .mcp.json: %v", err)
	}
}

func TestMCPServerStore_ListParsesStdioAndHTTPServers(t *testing.T) {
	root := t.TempDir()
	writeMCPJSON(t, root, "backend-developer", backendMCPJSON)
	manifestPath := writeManifest(t, root, Manifest{Agents: map[string]AgentConfig{
		"backend-developer": {MCP: "self"},
	}})

	got := NewMCPServerStore(manifestPath, root).List()
	if len(got) != 2 {
		t.Fatalf("expected 2 servers, got %d: %+v", len(got), got)
	}
	byName := map[string]*mcpserver.Server{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if byName["postgres"].Command != "uv" {
		t.Fatalf("postgres: %+v", byName["postgres"])
	}
	if byName["github"].Headers["Authorization"] != "Bearer ${GITHUB_PERSONAL_ACCESS_TOKEN}" {
		t.Fatalf("github header (secret placeholder must survive untouched): %+v", byName["github"])
	}
	if byName["postgres"].ID != "cfg:postgres" {
		t.Fatalf("postgres id: %s", byName["postgres"].ID)
	}
}

func TestMCPServerStore_ListReflectsLiveEditsAcrossCalls(t *testing.T) {
	root := t.TempDir()
	writeMCPJSON(t, root, "backend-developer", backendMCPJSON)
	manifestPath := writeManifest(t, root, Manifest{Agents: map[string]AgentConfig{
		"backend-developer": {MCP: "self"},
	}})
	store := NewMCPServerStore(manifestPath, root)

	if len(store.List()) != 2 {
		t.Fatalf("expected 2 servers before adding an agent")
	}

	writeManifest(t, root, Manifest{Agents: map[string]AgentConfig{
		"backend-developer": {MCP: "self"},
		"designer":          {},
	}})
	if len(store.List()) != 2 {
		t.Fatalf("expected the second read to still see 2 servers (designer has no mcp)")
	}
}

func TestMCPServerStore_ResolvesMCPPointingAtAnotherAgentsDir(t *testing.T) {
	root := t.TempDir()
	writeMCPJSON(t, root, "backend-developer", backendMCPJSON)
	agents := map[string]AgentConfig{
		"backend-developer": {MCP: "self"},
		"node-developer":    {MCP: "backend-developer"},
	}

	got := NewMCPServerStore(writeManifest(t, root, Manifest{Agents: agents}), root).
		serverNamesFor("node-developer", agents["node-developer"])
	if len(got) != 2 {
		t.Fatalf("expected node-developer to resolve to backend-developer's 2 servers, got %v", got)
	}
}

func TestMCPServerStore_NoServersWhenAgentHasNoMCP(t *testing.T) {
	cfg := AgentConfig{}
	got := NewMCPServerStore("unused", t.TempDir()).serverNamesFor("designer", cfg)
	if len(got) != 0 {
		t.Fatalf("expected no servers, got %v", got)
	}
}

func TestMCPServerStore_NoServersWhenFileMissing(t *testing.T) {
	cfg := AgentConfig{MCP: "self"}
	got := NewMCPServerStore("unused", t.TempDir()).serverNamesFor("ghost", cfg)
	if len(got) != 0 {
		t.Fatalf("expected no servers for a missing .mcp.json, got %v", got)
	}
}

func TestMCPServerStore_GetReturnsNilForNonConfigID(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeManifest(t, root, Manifest{Agents: map[string]AgentConfig{}})
	if got := NewMCPServerStore(manifestPath, root).Get("db-uuid"); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestMCPServerStore_WritesAreReadOnly(t *testing.T) {
	store := NewMCPServerStore("unused", t.TempDir())
	if _, err := store.Create(mcpserver.Input{}); err == nil {
		t.Fatal("expected Create to fail")
	}
	if _, err := store.Update("cfg:x", mcpserver.Input{}); err == nil {
		t.Fatal("expected Update to fail")
	}
	if _, err := store.UpdateProbeResult("cfg:x", mcpserver.ProbeResult{}); err == nil {
		t.Fatal("expected UpdateProbeResult to fail")
	}
	if err := store.Delete("cfg:x"); err == nil {
		t.Fatal("expected Delete to fail")
	}
}
