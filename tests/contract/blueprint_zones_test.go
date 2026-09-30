package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/persistence/memory"

	"github.com/rfbatista/harnesskit/skill"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/tests/testhelper"
)

func TestZones_ListCreateGetUpdateAssignPath(t *testing.T) {
	root := t.TempDir()
	projectStore := memory.NewProjectStore()
	zoneStore := memory.NewStore()
	agentStore := memory.NewAgentStore()
	promptStore := memory.NewPromptStore()
	skillStore := skill.NewMemoryStore()
	mcpServerStore := memory.NewMCPServerStore()
	pathMatcher := filesystem.NewMatcher()
	treeLister := filesystem.NewLister()
	cat := catalog.New(catalog.Deps{Projects: projectStore, Zones: zoneStore, Agents: agentStore, Prompts: promptStore, Skills: skillStore, MCPServers: mcpServerStore, PathMatcher: pathMatcher, TreeLister: treeLister, DefaultRoot: root})
	baseURL, cleanup := testhelper.StartMCPServer(t, &cat, false)
	defer cleanup()
	c := testhelper.NewTestClient(t, baseURL)
	defer c.Close()

	ctx := context.Background()

	// create_project first
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "create_project"
	callReq.Params.Arguments = map[string]any{"name": "testproj", "root_dir": root}
	res, err := c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	if res.IsError {
		t.Fatalf("create_project error: %v", res.Content)
	}
	var projOut struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(testhelper.ToolResultText(res.Content[0])), &projOut); err != nil {
		t.Fatalf("unmarshal create_project: %v", err)
	}
	projectID := projOut.Project.ID

	// list_zones empty
	callReq.Params.Name = "list_zones"
	callReq.Params.Arguments = map[string]any{"project_id": projectID}
	res, err = c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("list_zones: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_zones error: %v", res.Content)
	}
	var listOut struct {
		Zones []map[string]any `json:"zones"`
	}
	if err := json.Unmarshal([]byte(testhelper.ToolResultText(res.Content[0])), &listOut); err != nil {
		t.Fatalf("unmarshal list_zones: %v", err)
	}
	if len(listOut.Zones) != 0 {
		t.Errorf("expected 0 zones, got %d", len(listOut.Zones))
	}

	// create_zone
	callReq.Params.Name = "create_zone"
	callReq.Params.Arguments = map[string]any{
		"project_id": projectID,
		"name":       "backend",
		"pattern":    "cmd/.*",
		"purpose":    "Server and CLI",
	}
	res, err = c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("create_zone: %v", err)
	}
	if res.IsError {
		t.Fatalf("create_zone error: %v", res.Content)
	}
	var createOut struct {
		Zone struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Pattern string `json:"pattern"`
		} `json:"zone"`
	}
	if err := json.Unmarshal([]byte(testhelper.ToolResultText(res.Content[0])), &createOut); err != nil {
		t.Fatalf("unmarshal create_zone: %v", err)
	}
	if createOut.Zone.ID == "" {
		t.Error("expected zone id")
	}
	if createOut.Zone.Name != "backend" {
		t.Errorf("expected name backend, got %q", createOut.Zone.Name)
	}
	zoneID := createOut.Zone.ID

	// get_zone
	callReq.Params.Name = "get_zone"
	callReq.Params.Arguments = map[string]any{"zone_id": zoneID}
	res, err = c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("get_zone: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_zone error: %v", res.Content)
	}
	var getOut struct {
		Zone struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"zone"`
	}
	if err := json.Unmarshal([]byte(testhelper.ToolResultText(res.Content[0])), &getOut); err != nil {
		t.Fatalf("unmarshal get_zone: %v", err)
	}
	if getOut.Zone.Name != "backend" {
		t.Errorf("get_zone name: got %q", getOut.Zone.Name)
	}

	// update_zone
	callReq.Params.Name = "update_zone"
	callReq.Params.Arguments = map[string]any{"zone_id": zoneID, "name": "backend-updated"}
	res, err = c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("update_zone: %v", err)
	}
	if res.IsError {
		t.Fatalf("update_zone error: %v", res.Content)
	}

	// assign_path_to_zone
	callReq.Params.Name = "assign_path_to_zone"
	callReq.Params.Arguments = map[string]any{"zone_id": zoneID, "path": "internal/blueprint"}
	res, err = c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("assign_path_to_zone: %v", err)
	}
	if res.IsError {
		t.Fatalf("assign_path_to_zone error: %v", res.Content)
	}
	var assignOut struct {
		Zone struct {
			ExplicitPaths []string `json:"explicit_paths"`
		} `json:"zone"`
	}
	if err := json.Unmarshal([]byte(testhelper.ToolResultText(res.Content[0])), &assignOut); err != nil {
		t.Fatalf("unmarshal assign: %v", err)
	}
	if len(assignOut.Zone.ExplicitPaths) != 1 || assignOut.Zone.ExplicitPaths[0] != "internal/blueprint" {
		t.Errorf("expected explicit_paths [internal/blueprint], got %v", assignOut.Zone.ExplicitPaths)
	}

	// list_zones now has one
	callReq.Params.Name = "list_zones"
	callReq.Params.Arguments = map[string]any{"project_id": projectID}
	res, err = c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("list_zones: %v", err)
	}
	if err := json.Unmarshal([]byte(testhelper.ToolResultText(res.Content[0])), &listOut); err != nil {
		t.Fatalf("unmarshal list_zones: %v", err)
	}
	if len(listOut.Zones) != 1 {
		t.Errorf("expected 1 zone, got %d", len(listOut.Zones))
	}
}

func TestGetZone_NotFound_StructuredError(t *testing.T) {
	root := t.TempDir()
	projectStore := memory.NewProjectStore()
	zoneStore := memory.NewStore()
	agentStore := memory.NewAgentStore()
	promptStore := memory.NewPromptStore()
	skillStore := skill.NewMemoryStore()
	mcpServerStore := memory.NewMCPServerStore()
	pathMatcher := filesystem.NewMatcher()
	treeLister := filesystem.NewLister()
	cat := catalog.New(catalog.Deps{Projects: projectStore, Zones: zoneStore, Agents: agentStore, Prompts: promptStore, Skills: skillStore, MCPServers: mcpServerStore, PathMatcher: pathMatcher, TreeLister: treeLister, DefaultRoot: root})
	baseURL, cleanup := testhelper.StartMCPServer(t, &cat, false)
	defer cleanup()
	c := testhelper.NewTestClient(t, baseURL)
	defer c.Close()

	res, err := c.CallTool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "get_zone", Arguments: map[string]any{"zone_id": "nonexistent"}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for nonexistent zone")
	}
}
