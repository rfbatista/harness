package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/persistence/memory"

	"github.com/rfbatista/harnesskit/skill"
	"operators-mcp/internal/application/blueprint"
	"operators-mcp/tests/testhelper"
)

func setupFilesystemServer(t *testing.T) (c *client.Client, root string, cleanup func()) {
	t.Helper()
	root = t.TempDir()
	projectStore := memory.NewProjectStore()
	zoneStore := memory.NewStore()
	agentStore := memory.NewAgentStore()
	promptStore := memory.NewPromptStore()
	skillStore := skill.NewMemoryStore()
	mcpServerStore := memory.NewMCPServerStore()
	pathMatcher := filesystem.NewMatcher()
	treeLister := filesystem.NewLister()
	svc := blueprint.NewService(projectStore, nil, zoneStore, agentStore, promptStore, skillStore, mcpServerStore, nil, pathMatcher, treeLister, root)
	baseURL, serverCleanup := testhelper.StartMCPServer(t, svc, false)
	c = testhelper.NewTestClient(t, baseURL)
	return c, root, func() { c.Close(); serverCleanup() }
}

func callTool(t *testing.T, c *client.Client, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := c.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func parseResult(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	text := testhelper.ToolResultText(res.Content[0])
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, text)
	}
	return out
}

// TestReadFile_LineNumbers verifies line-numbered output with metadata.
func TestReadFile_LineNumbers(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "test.go")
	_ = os.WriteFile(filePath, []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644)

	res := callTool(t, c, "read_file", map[string]any{"path": filePath})
	out := parseResult(t, res)

	content, ok := out["content"].(string)
	if !ok || content == "" {
		t.Fatal("expected content")
	}
	if !strings.Contains(content, "1|package main") {
		t.Errorf("expected line-numbered output, got:\n%s", content[:min(200, len(content))])
	}

	totalLines, ok := out["total_lines"].(float64)
	if !ok || totalLines < 7 {
		t.Errorf("expected total_lines >= 7, got %v", out["total_lines"])
	}

	lang, _ := out["language"].(string)
	if lang != "go" {
		t.Errorf("expected language=go, got %q", lang)
	}
}

// TestReadFile_PartialRange verifies start_line/end_line support.
func TestReadFile_PartialRange(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "lines.txt")
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, "line "+strings.Repeat("x", i))
	}
	_ = os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	res := callTool(t, c, "read_file", map[string]any{
		"path":       filePath,
		"start_line": 5,
		"end_line":   10,
	})
	out := parseResult(t, res)

	returnedRange, ok := out["returned_range"].([]any)
	if !ok || len(returnedRange) != 2 {
		t.Fatalf("expected returned_range [5, 10], got %v", out["returned_range"])
	}
	if int(returnedRange[0].(float64)) != 5 || int(returnedRange[1].(float64)) != 10 {
		t.Errorf("expected [5,10], got %v", returnedRange)
	}
}

// TestEditFile_ExactReplace verifies the basic exact match flow with diff output.
func TestEditFile_ExactReplace(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "edit.go")
	_ = os.WriteFile(filePath, []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644)

	res := callTool(t, c, "edit_file", map[string]any{
		"path":    filePath,
		"old_str": `fmt.Println("hello")`,
		"new_str": `fmt.Println("world")`,
	})
	out := parseResult(t, res)

	if out["result"] != "OK" {
		t.Errorf("expected OK, got %v", out["result"])
	}
	if out["strategy"] != "exact" {
		t.Errorf("expected exact strategy, got %v", out["strategy"])
	}
	diff, _ := out["diff"].(string)
	if diff == "" {
		t.Error("expected non-empty diff")
	}

	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), `fmt.Println("world")`) {
		t.Error("file should contain replacement")
	}
}

// TestEditFile_WhitespaceFlexMatch verifies whitespace-flexible matching.
func TestEditFile_WhitespaceFlexMatch(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "ws.go")
	_ = os.WriteFile(filePath, []byte("package main\n\nfunc main() {\n    if true {\n        doStuff()\n    }\n}\n"), 0644)

	res := callTool(t, c, "edit_file", map[string]any{
		"path":    filePath,
		"old_str": "if true {\n    doStuff()\n}",
		"new_str": "if true {\n    doOther()\n}",
	})
	out := parseResult(t, res)

	if out["strategy"] != "whitespace_flex" {
		t.Errorf("expected whitespace_flex strategy, got %v", out["strategy"])
	}

	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), "doOther()") {
		t.Error("expected replacement in file")
	}
}

// TestEditFile_NotFound_DidYouMean verifies "did you mean" suggestions.
func TestEditFile_NotFound_DidYouMean(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "suggest.txt")
	_ = os.WriteFile(filePath, []byte("func handler() {\n\ta := 1\n\tb := 2\n\tc := 3\n\td := 4\n}\n"), 0644)

	res := callTool(t, c, "edit_file", map[string]any{
		"path":    filePath,
		"old_str": "func handler() {\n\tx := 99\n\ty := 88\n\tz := 77\n\tw := 66\n}",
		"new_str": "replacement",
	})

	if !res.IsError {
		t.Fatal("expected error for non-matching old_str")
	}
	text := testhelper.ToolResultText(res.Content[0])
	if !strings.Contains(strings.ToLower(text), "similar") && !strings.Contains(strings.ToLower(text), "not found") {
		t.Errorf("expected error message about search text, got: %s", text)
	}
}

// TestEditFile_CreateIfMissing verifies file creation when create_if_missing is true.
func TestEditFile_CreateIfMissing(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "subdir", "new.txt")

	res := callTool(t, c, "edit_file", map[string]any{
		"path":              filePath,
		"old_str":           "",
		"new_str":           "hello world\n",
		"create_if_missing": true,
	})
	out := parseResult(t, res)

	if out["result"] != "OK" {
		t.Errorf("expected OK, got %v", out["result"])
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if string(data) != "hello world\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

// TestWriteFile_NewFile verifies creating a new file.
func TestWriteFile_NewFile(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "created.txt")

	res := callTool(t, c, "write_file", map[string]any{
		"path":    filePath,
		"content": "line 1\nline 2\nline 3\n",
	})
	out := parseResult(t, res)

	if out["result"] != "OK" {
		t.Errorf("expected OK, got %v", out["result"])
	}
	linesWritten := out["lines_written"].(float64)
	if int(linesWritten) != 3 {
		t.Errorf("expected 3 lines_written, got %v", linesWritten)
	}
}

// TestWriteFile_ExistingFile_NoOverwrite verifies error when file exists.
func TestWriteFile_ExistingFile_NoOverwrite(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "existing.txt")
	_ = os.WriteFile(filePath, []byte("original"), 0644)

	res := callTool(t, c, "write_file", map[string]any{
		"path":    filePath,
		"content": "overwritten",
	})

	if !res.IsError {
		t.Fatal("expected error when file exists and overwrite is false")
	}
}

// TestWriteFile_Overwrite verifies overwriting with diff.
func TestWriteFile_Overwrite(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "overwrite.txt")
	_ = os.WriteFile(filePath, []byte("old content\n"), 0644)

	res := callTool(t, c, "write_file", map[string]any{
		"path":      filePath,
		"content":   "new content\n",
		"overwrite": true,
	})
	out := parseResult(t, res)

	if out["result"] != "OK" {
		t.Errorf("expected OK, got %v", out["result"])
	}
	diff, _ := out["diff"].(string)
	if diff == "" {
		t.Error("expected diff for overwrite")
	}

	data, _ := os.ReadFile(filePath)
	if string(data) != "new content\n" {
		t.Errorf("file not overwritten: %q", string(data))
	}
}

// TestListFiles_DepthLimit verifies max_depth is respected.
func TestListFiles_DepthLimit(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	_ = os.MkdirAll(filepath.Join(root, "a", "b", "c"), 0755)
	_ = os.WriteFile(filepath.Join(root, "a", "top.txt"), nil, 0644)
	_ = os.WriteFile(filepath.Join(root, "a", "b", "mid.txt"), nil, 0644)
	_ = os.WriteFile(filepath.Join(root, "a", "b", "c", "deep.txt"), nil, 0644)

	res := callTool(t, c, "list_files", map[string]any{
		"path":      root,
		"max_depth": 2,
	})
	out := parseResult(t, res)

	entries, ok := out["entries"].([]any)
	if !ok {
		t.Fatalf("expected entries array, got %T", out["entries"])
	}

	for _, e := range entries {
		entry := e.(map[string]any)
		path := entry["path"].(string)
		if strings.Contains(path, "deep.txt") {
			t.Error("deep.txt should not appear with max_depth=2")
		}
	}
}

// TestListFiles_GitignoreRespected verifies .gitignore patterns are honored.
func TestListFiles_GitignoreRespected(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\nnode_modules/\n"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0755)
	_ = os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), nil, 0644)
	_ = os.WriteFile(filepath.Join(root, "app.go"), nil, 0644)
	_ = os.WriteFile(filepath.Join(root, "debug.log"), nil, 0644)

	res := callTool(t, c, "list_files", map[string]any{"path": root})
	out := parseResult(t, res)

	entries := out["entries"].([]any)
	for _, e := range entries {
		entry := e.(map[string]any)
		path := entry["path"].(string)
		if strings.Contains(path, "node_modules") {
			t.Errorf("node_modules should be ignored, found: %s", path)
		}
		if strings.HasSuffix(path, ".log") {
			t.Errorf("*.log should be ignored, found: %s", path)
		}
	}

	found := false
	for _, e := range entries {
		entry := e.(map[string]any)
		if entry["path"] == "app.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("app.go should be listed")
	}
}

// TestListFiles_IncludePattern verifies glob include filtering.
func TestListFiles_IncludePattern(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	_ = os.WriteFile(filepath.Join(root, "main.go"), nil, 0644)
	_ = os.WriteFile(filepath.Join(root, "readme.md"), nil, 0644)
	_ = os.WriteFile(filepath.Join(root, "style.css"), nil, 0644)

	res := callTool(t, c, "list_files", map[string]any{
		"path":            root,
		"include_pattern": "*.go",
	})
	out := parseResult(t, res)

	entries := out["entries"].([]any)
	for _, e := range entries {
		entry := e.(map[string]any)
		path := entry["path"].(string)
		entryType := entry["type"].(string)
		if entryType == "file" && !strings.HasSuffix(path, ".go") {
			t.Errorf("non-.go file found: %s", path)
		}
	}
}

// TestToolsListIncludesNewTools verifies all filesystem + repo_map tools appear in tools/list.
func TestToolsListIncludesNewTools(t *testing.T) {
	c, _, cleanup := setupFilesystemServer(t)
	defer cleanup()

	listRes, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	want := map[string]bool{
		"read_file":  true,
		"list_files": true,
		"edit_file":  true,
		"write_file": true,
		"repo_map":   true,
	}
	for _, tool := range listRes.Tools {
		delete(want, tool.Name)
	}
	if len(want) > 0 {
		t.Errorf("missing tools: %v", want)
	}
}

// TestRepoMap_ExtractsSymbols verifies the repo_map tool scans files and extracts symbols.
func TestRepoMap_ExtractsSymbols(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	_ = os.MkdirAll(filepath.Join(root, "pkg"), 0755)
	_ = os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {\n}\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "pkg", "service.go"), []byte("package pkg\n\ntype Service struct{}\n\nfunc NewService() *Service { return nil }\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "app.ts"), []byte("interface User { name: string; }\n\nexport function getUser(): User { return { name: \"\" }; }\n"), 0644)

	res := callTool(t, c, "repo_map", map[string]any{"path": root})
	out := parseResult(t, res)

	repoMap, ok := out["map"].(string)
	if !ok || repoMap == "" {
		t.Fatal("expected non-empty repo map")
	}

	if !strings.Contains(repoMap, "main") {
		t.Error("repo map should contain main function")
	}
	if !strings.Contains(repoMap, "Service") {
		t.Error("repo map should contain Service type")
	}
	if !strings.Contains(repoMap, "NewService") {
		t.Error("repo map should contain NewService function")
	}
	if !strings.Contains(repoMap, "User") {
		t.Error("repo map should contain User interface")
	}

	totalSymbols, _ := out["total_symbols"].(float64)
	if totalSymbols < 3 {
		t.Errorf("expected at least 3 symbols, got %v", totalSymbols)
	}
}

// TestRepoMap_QueryFilter verifies that query ranks relevant files higher.
func TestRepoMap_QueryFilter(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	_ = os.WriteFile(filepath.Join(root, "auth.go"), []byte("package main\n\nfunc Authenticate() {}\n\nfunc Authorize() {}\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "utils.go"), []byte("package main\n\nfunc Helper() {}\n"), 0644)

	res := callTool(t, c, "repo_map", map[string]any{
		"path":  root,
		"query": "auth",
	})
	out := parseResult(t, res)

	repoMap := out["map"].(string)
	authIdx := strings.Index(repoMap, "auth.go")
	utilsIdx := strings.Index(repoMap, "utils.go")

	if authIdx < 0 {
		t.Fatal("auth.go should appear in repo map")
	}
	if utilsIdx >= 0 && authIdx > utilsIdx {
		t.Error("auth.go should appear before utils.go when query is 'auth'")
	}
}

// TestEditFile_TreeSitterLintWarning verifies tree-sitter lint shows syntax errors after edits.
func TestEditFile_TreeSitterLintWarning(t *testing.T) {
	c, root, cleanup := setupFilesystemServer(t)
	defer cleanup()

	filePath := filepath.Join(root, "broken.go")
	_ = os.WriteFile(filePath, []byte("package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"), 0644)

	res := callTool(t, c, "edit_file", map[string]any{
		"path":    filePath,
		"old_str": "x := 1",
		"new_str": "x :=",
	})
	out := parseResult(t, res)

	if out["result"] != "OK" {
		t.Errorf("expected OK, got %v", out["result"])
	}

	warnings, _ := out["lint_warnings"].(string)
	if !strings.Contains(strings.ToLower(warnings), "syntax") && !strings.Contains(strings.ToLower(warnings), "error") {
		t.Errorf("expected tree-sitter syntax error in lint_warnings, got: %q", warnings)
	}
}
