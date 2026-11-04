package agentcatalog_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"operators-mcp/internal/adapter/out/agentcatalog"
)

// The cheap read of the agents, which does not scan the skill folders, answers
// the same agents as the whole-catalog read: their names, descriptions,
// aliases, entries, resolved skills, MCP servers and allowed tools.
func TestTheAgentsReadAnswersTheAgentsOfTheWholeCatalogRead(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("skills/present/SKILL.md", "---\nname: present\ndescription: Here.\n---\n\nBody.\n")
	write("skills/present/references/a.md", "A reference.\n")
	write("mcp-servers.json", `{"mcpServers": {"known": {"command": "/bin/true"}}}`)
	// An agent with a bundle, a skill that does not exist, a repeated skill, an
	// unknown MCP server, an alias and allowed tools; one with no description.
	write("agents.json", `{"agents": {
		"builder": {"description": "Builds.", "aliases": ["b"], "skills": ["@kit", "gone", "present"], "mcp_servers": ["known", "unknown"], "allowed_tools": ["Read", "Bash"]},
		"bare": {}
	}, "bundles": {"kit": ["present"]}}`)

	cat := agentcatalog.New(root)
	full, err := cat.ReadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	got, err := cat.ReadCatalogAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(full.Agents) || len(got) != 2 {
		t.Fatalf("agents = %d, whole read %d, want 2", len(got), len(full.Agents))
	}
	for i, want := range full.Agents {
		a := got[i]
		if a.Name != want.Name || a.Description != want.Description ||
			!reflect.DeepEqual(a.Aliases, want.Aliases) || !reflect.DeepEqual(a.Entries, want.Entries) ||
			!reflect.DeepEqual(a.ResolvedSkills, want.ResolvedSkills) || !reflect.DeepEqual(a.MCPServers, want.MCPServers) ||
			!reflect.DeepEqual(a.AllowedTools, want.AllowedTools) {
			t.Errorf("agent %d: cheap read %+v, whole read %+v", i, a, want)
		}
	}
}
