package catalog

import (
	tea "charm.land/bubbletea/v2"

	"errors"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/domain"
)

func mcpsFixture() (*backend.Fake, Model) {
	f := backend.NewFake()
	f.MCPServers = []*domain.MCPServer{{ID: "m1", Name: "filesystem", Transport: domain.MCPTransportStdio, Command: "npx", Args: []string{"-y", "fs"}, Env: map[string]string{"ROOT": "/src"}}}
	m := withSnap(New(ctx(), MCPs, f), f)
	return f, m
}

func TestMCPNewCreatesWithArgsAndEnv(t *testing.T) {
	f, m := mcpsFixture()
	m, _ = press(m, ch('n'))
	m = typeKeys(m, "github")
	m, _ = press(m, tab()) // description
	m, _ = press(m, tab()) // transport select (stdio default)
	m, _ = press(m, tab()) // command
	m = typeKeys(m, "npx")
	m, _ = press(m, tab()) // args
	m = typeKeys(m, "-y @mcp/github")
	m, _ = press(m, tab()) // url
	m, _ = press(m, tab()) // env multiline
	m, _ = m.Update(editorDone("env", "TOKEN=abc\nDEBUG=1"))
	_, root := press(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if len(f.MCPServers) != 2 || !hasRefresh(root) {
		t.Fatalf("server not created: %+v", f.MCPServers)
	}
	s := f.MCPServers[1]
	if s.Command != "npx" || len(s.Args) != 2 || s.Args[1] != "@mcp/github" || s.Env["TOKEN"] != "abc" || s.Env["DEBUG"] != "1" {
		t.Fatalf("server shape: %+v", s)
	}
}

func TestMCPEditPrefillsArgsAndEnv(t *testing.T) {
	f, m := mcpsFixture()
	m, _ = press(m, ch('e'))
	v := m.View()
	if !strings.Contains(v, "filesystem") || !strings.Contains(v, "-y fs") {
		t.Fatalf("edit prefilled:\n%s", v)
	}
	m = typeKeys(m, "-2")
	_, _ = press(m, enter())
	if f.MCPServers[0].Name != "filesystem-2" || f.MCPServers[0].Env["ROOT"] != "/src" {
		t.Fatalf("update should keep env: %+v", f.MCPServers[0])
	}
}

func TestMCPTestProbesAndToasts(t *testing.T) {
	f, m := mcpsFixture()
	f.Probe = func(string) (domain.MCPProbeResult, error) {
		return domain.MCPProbeResult{Status: domain.MCPProbeStatusOK, ToolCount: 7, ToolNames: []string{"read", "write"}}, nil
	}
	_, root := press(m, ch('t'))
	var toast core.ToastMsg
	for _, r := range root {
		if tm, ok := r.(core.ToastMsg); ok {
			toast = tm
		}
	}
	if !strings.Contains(toast.Text, "7 tools") || toast.IsError || !hasRefresh(root) {
		t.Fatalf("probe toast: %+v root=%v", toast, root)
	}
}

func TestMCPTestFailureToastsError(t *testing.T) {
	f, m := mcpsFixture()
	f.Probe = func(string) (domain.MCPProbeResult, error) {
		return domain.MCPProbeResult{}, errors.New("connection refused")
	}
	_, root := press(m, ch('t'))
	found := false
	for _, r := range root {
		if tm, ok := r.(core.ToastMsg); ok && tm.IsError && strings.Contains(tm.Text, "connection refused") {
			found = true
		}
	}
	if !found {
		t.Fatalf("error toast expected, got %v", root)
	}
}

func TestMCPImportWithPolicy(t *testing.T) {
	f, m := mcpsFixture()
	m, _ = press(m, ch('i'))
	m, _ = m.Update(editorDone("content", `{"mcpServers": {"filesystem": {"command": "x"}, "web": {"url": "http://h/mcp"}}}`))
	m, _ = press(m, tab())   // policy select
	m, _ = press(m, ch(' ')) // skip → update
	m, _ = press(m, ch(' ')) // update → rename
	_, root := press(m, enter())
	if len(f.MCPServers) != 3 || !hasRefresh(root) {
		t.Fatalf("import with rename should add two: %+v", f.MCPServers)
	}
}

func TestMCPDeleteConfirms(t *testing.T) {
	f, m := mcpsFixture()
	m, _ = press(m, ch('D'))
	_, _ = press(m, ch('y'))
	if len(f.MCPServers) != 0 {
		t.Fatalf("server should be deleted: %+v", f.MCPServers)
	}
}

func TestMCPDetailShowsConfigAndProbeTools(t *testing.T) {
	f, m := mcpsFixture()
	f.Probe = func(string) (domain.MCPProbeResult, error) {
		return domain.MCPProbeResult{Status: domain.MCPProbeStatusOK, ToolCount: 2, ToolNames: []string{"read_file", "write_file"}, ServerName: "fs-server", ServerVersion: "1.2"}, nil
	}
	m, _ = press(m, enter())
	v := m.View()
	for _, want := range []string{"filesystem", "stdio", "npx -y fs", "ROOT=/src", "never probed"} {
		if !strings.Contains(v, want) {
			t.Fatalf("detail missing %q:\n%s", want, v)
		}
	}
	m, _ = press(m, ch('t'))
	v = m.View()
	for _, want := range []string{"read_file", "write_file", "fs-server 1.2"} {
		if !strings.Contains(v, want) {
			t.Fatalf("after probe missing %q:\n%s", want, v)
		}
	}
	m, _ = press(m, esc())
	if strings.Contains(m.View(), "read_file") {
		t.Fatal("esc should leave the detail")
	}
}
