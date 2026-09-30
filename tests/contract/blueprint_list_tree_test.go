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

func TestListTree_ValidRoot_ReturnsTree(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "cmd", "server"), 0755)
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
	callReq.Params.Name = "list_tree"
	callReq.Params.Arguments = map[string]any{}
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
		Tree struct {
			Path     string `json:"path"`
			Name     string `json:"name"`
			IsDir    bool   `json:"is_dir"`
			Children []any  `json:"children"`
		} `json:"tree"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Tree.Name != "." {
		t.Errorf("expected root name \".\", got %q", out.Tree.Name)
	}
	if !out.Tree.IsDir {
		t.Error("expected root to be dir")
	}
	if len(out.Tree.Children) == 0 {
		t.Error("expected at least one child (cmd or go.mod)")
	}
}

func TestListTree_UnreadableRoot_StructuredError(t *testing.T) {
	projectStore := memory.NewProjectStore()
	zoneStore := memory.NewStore()
	agentStore := memory.NewAgentStore()
	promptStore := memory.NewPromptStore()
	skillStore := skill.NewMemoryStore()
	mcpServerStore := memory.NewMCPServerStore()
	pathMatcher := filesystem.NewMatcher()
	treeLister := filesystem.NewLister()
	cat := catalog.New(catalog.Deps{Projects: projectStore, Zones: zoneStore, Agents: agentStore, Prompts: promptStore, Skills: skillStore, MCPServers: mcpServerStore, PathMatcher: pathMatcher, TreeLister: treeLister, DefaultRoot: "/nonexistent/path/12345"})
	baseURL, cleanup := testhelper.StartMCPServer(t, &cat, false)
	defer cleanup()
	c := testhelper.NewTestClient(t, baseURL)
	defer c.Close()

	ctx := context.Background()
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "list_tree"
	callReq.Params.Arguments = map[string]any{}
	res, err := c.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool to return error for unreadable root")
	}
}
