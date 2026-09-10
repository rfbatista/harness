package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/persistence/memory"

	"github.com/rfbatista/harnesskit/skill"
	"operators-mcp/internal/application/blueprint"
	"operators-mcp/tests/testhelper"
)

// TestBlueprintTools_WithDesignerResource runs server with both ui://designer
// and blueprint tools; invokes list_tree and list_zones to verify integration.
func TestBlueprintTools_WithDesignerResource(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "cmd"), 0755)
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0644)

	projectStore := memory.NewProjectStore()
	zoneStore := memory.NewStore()
	agentStore := memory.NewAgentStore()
	promptStore := memory.NewPromptStore()
	skillStore := skill.NewMemoryStore()
	mcpServerStore := memory.NewMCPServerStore()
	pathMatcher := filesystem.NewMatcher()
	treeLister := filesystem.NewLister()
	svc := blueprint.NewService(projectStore, nil, zoneStore, agentStore, promptStore, skillStore, mcpServerStore, nil, pathMatcher, treeLister, root)
	baseURL, cleanup := testhelper.StartMCPServer(t, svc, false)
	defer cleanup()
	c := testhelper.NewTestClient(t, baseURL)
	defer c.Close()

	ctx := context.Background()

	// Verify tools are visible via tools/list (e.g. for Cursor/IDE discovery).
	listRes, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	wantNames := map[string]bool{
		"list_projects": true, "get_project": true, "create_project": true, "update_project": true, "delete_project": true,
		"add_ignored_path": true, "remove_ignored_path": true,
		"list_matching_paths": true, "list_tree": true, "list_zones": true,
		"get_zone": true, "create_zone": true, "update_zone": true, "assign_path_to_zone": true,
		"list_agents": true, "get_agent": true, "create_agent": true, "update_agent": true, "delete_agent": true,
	}
	if len(listRes.Tools) < len(wantNames) {
		t.Fatalf("ListTools: got %d tools, want at least %d", len(listRes.Tools), len(wantNames))
	}
	for _, tool := range listRes.Tools {
		if !wantNames[tool.Name] {
			continue
		}
		delete(wantNames, tool.Name)
	}
	if len(wantNames) != 0 {
		t.Errorf("ListTools: missing tools: %v", wantNames)
	}

	// Call list_tree
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "list_tree"
	callReq.Params.Arguments = map[string]any{}
	res, err := c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("list_tree: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_tree error: %v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("expected list_tree content")
	}
	text := testhelper.ToolResultText(res.Content[0])
	var treeOut struct {
		Tree struct {
			Name string `json:"name"`
		} `json:"tree"`
	}
	if err := json.Unmarshal([]byte(text), &treeOut); err != nil {
		t.Fatalf("unmarshal list_tree: %v", err)
	}
	if treeOut.Tree.Name != "." {
		t.Errorf("list_tree root name: got %q", treeOut.Tree.Name)
	}

	// Create a project so we can list zones
	p, err := svc.CreateProject("testproj", root)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Call list_zones
	callReq.Params.Name = "list_zones"
	callReq.Params.Arguments = map[string]any{"project_id": p.ID}
	res, err = c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("list_zones: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_zones error: %v", res.Content)
	}
}
