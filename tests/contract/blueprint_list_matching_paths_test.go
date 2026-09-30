package contract

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
	"operators-mcp/internal/app/catalog"
	"operators-mcp/tests/testhelper"
)

func TestListMatchingPaths_ValidPattern_ReturnsPaths(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "cmd", "server"), 0755)
	_ = os.MkdirAll(filepath.Join(root, "internal", "mcp"), 0755)
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0644)

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
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "list_matching_paths"
	callReq.Params.Arguments = map[string]any{"pattern": "cmd"}
	res, err := c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("expected content")
	}
	text := testhelper.ToolResultText(res.Content[0])
	var out struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, p := range out.Paths {
		if p == "cmd" || p == "cmd/server" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected paths to contain cmd or cmd/server, got %v", out.Paths)
	}
}

func TestListMatchingPaths_InvalidPattern_StructuredError(t *testing.T) {
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
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "list_matching_paths"
	callReq.Params.Arguments = map[string]any{"pattern": "["}
	res, err := c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool to return error for invalid pattern")
	}
}
