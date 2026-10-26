package httpapi

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/agentcatalog"
)

func TestCatalogSkills_APreviousNameThatLeavesTheCatalogIsRefusedAndTouchesNothing(t *testing.T) {
	f := catalogWithSkills(t)
	outside := filepath.Join(filepath.Dir(f.dir), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := f.post("/api/save_catalog_skill", map[string]any{"previous_name": "../outside", "skill": skillBody("x", "d")}, 404)
	if got["code"] != "SKILL_NOT_FOUND" {
		t.Fatalf("body = %v", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatal("a file outside the catalog was removed")
	}
	f.post("/api/delete_catalog_skill", map[string]any{"name": "../outside"}, 404)
}

func TestCatalogImport_ADotfileIsNotContentAndAnMCPFolderOutsideTheSourceIsDropped(t *testing.T) {
	repo := importFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "agents.json"), []byte(`{"agents":{"a":{"skills":[],"mcp":"../escape"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newCatalogFixture(t)
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f.write(".DS_Store", "x")
	rep, err := agentcatalog.New(f.dir).Import(agentcatalog.ImportSource{RepoDir: repo})
	if err != nil || rep.Skipped || len(rep.Dropped) != 1 {
		t.Fatalf("import = %+v, %v", rep, err)
	}
	if got := strList(byName(t, f.get("/api/get_agent_catalog", 200)["agents"], "a")["mcp_servers"]); len(got) != 0 {
		t.Fatalf("servers = %v", got)
	}
}

func TestCatalogMCPServers_RenameKeepsThePlaceInAnAgentsList(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("mcp-servers.json", `{"mcpServers":{"a":{"command":"x"},"b":{"command":"y"},"c":{"command":"z"}}}`)
	f.write("agents.json", `{"agents":{"ag":{"description":"d","mcp_servers":["a","b","c"]}}}`)
	f.post("/api/save_catalog_mcp_server", map[string]any{"previous_name": "b", "server": map[string]any{"name": "bb", "command": "y"}}, 200)
	got := strList(byName(t, f.get("/api/get_agent_catalog", 200)["agents"], "ag")["mcp_servers"])
	if !reflect.DeepEqual(got, []string{"a", "bb", "c"}) {
		t.Fatalf("servers = %v", got)
	}
	if strings.Contains(f.read("mcp-servers.json"), `"b":`) {
		t.Fatal("the old name is still in the file")
	}
}
