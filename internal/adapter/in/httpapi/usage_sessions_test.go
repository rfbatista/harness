package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
	"github.com/rfbatista/llmkit/claude/mcpapprove"

	"operators-mcp/internal/adapter/out/agents/claudecli"
	"operators-mcp/internal/adapter/out/agents/command"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// A headless session through the LLM proxy (seam H1): the claude it starts
// calls through the proxy with its session id as attribution, and its totals
// count the cache tokens the usage records count.

const testProxyURL = "http://127.0.0.1:18080/llm"

func proxiedSessionServer(t *testing.T, env ...string) string {
	t.Helper()
	return proxiedSessions(t, env...).url
}

type proxiedEnv struct {
	url string
	svc *orchestration.Service
	mgr *claude.Manager
}

func proxiedSessions(t *testing.T, env ...string) proxiedEnv {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	mgr := claude.New(llmkit.Options{
		Bin:             exe,
		ApprovalURL:     func(id string) string { return "http://127.0.0.1:8080" + mcpapprove.PathPrefix + id },
		ApprovalTimeout: time.Hour,
		ProxyURL:        testProxyURL,
	})
	res := &stubResolver{proj: &domain.Project{ID: "p1", RootDir: t.TempDir()}}
	svc := orchestration.NewService(mgr, mgr.Approvals(), orchestration.NewHub(64), sqlite.NewSessionRepository(db),
		orchestration.Catalog{Projects: res, Repositories: res, Agents: res, MCPServers: res, Zones: res},
		stubTickets{"tk1": {ID: "tk1", ProjectID: "p1", Title: "Ship the thing"}}, &stubProvisioner{dir: t.TempDir()})
	svc.DefaultEnv = append([]string{"CLAUDE_FAKE=1"}, env...)
	t.Cleanup(func() { mgr.StopAll() })
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: svc}))
	t.Cleanup(srv.Close)
	return proxiedEnv{url: srv.URL, svc: svc, mgr: mgr}
}

func startSession(t *testing.T, base string) string {
	t.Helper()
	resp, err := http.Post(base+"/api/sessions", "application/json",
		strings.NewReader(`{"repository_id":"r1","project_id":"p1","task":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var created struct {
		Session domain.Session `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.Session.ID == "" {
		t.Fatalf("create session = %d %v", resp.StatusCode, err)
	}
	return created.Session.ID
}

func TestAHeadlessSessionCallsThroughTheProxyAsItself(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env")
	base := proxiedSessionServer(t, "CLAUDE_FAKE_ENV_FILE="+envFile)
	id := startSession(t, base)

	var env string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(envFile); err == nil && len(b) > 0 {
			env = string(b)
			break
		}
	}
	lines := strings.Split(env, "\n")
	var baseURL, headers string
	for _, l := range lines {
		if v, ok := strings.CutPrefix(l, "ANTHROPIC_BASE_URL="); ok {
			baseURL = v
		}
		if v, ok := strings.CutPrefix(l, "ANTHROPIC_CUSTOM_HEADERS="); ok {
			headers = v
		}
	}
	if baseURL != testProxyURL {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want the proxy %q", baseURL, testProxyURL)
	}
	if !strings.Contains(headers, "X-Llm-Attribution: "+id) {
		t.Errorf("ANTHROPIC_CUSTOM_HEADERS = %q, want the session's id %s as attribution", headers, id)
	}
}

func TestASessionsTotalsCountItsCacheTokens(t *testing.T) {
	base := proxiedSessionServer(t, "CLAUDE_FAKE_USAGE=1")
	id := startSession(t, base)

	var s domain.Session
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		resp, err := http.Get(base + "/api/sessions/" + id)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Session domain.Session `json:"session"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if s = got.Session; s.InputTokens > 0 {
			break
		}
	}
	if s.InputTokens != 10 || s.OutputTokens != 20 || s.CacheCreationInputTokens != 300 || s.CacheReadInputTokens != 4000 {
		t.Errorf("session totals = in %d out %d cache write %d cache read %d, want 10/20/300/4000",
			s.InputTokens, s.OutputTokens, s.CacheCreationInputTokens, s.CacheReadInputTokens)
	}
}

// waitFile answers path's content once it is written.
func waitFile(t *testing.T, path string) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
	}
	t.Fatalf("%s was never written", path)
	return ""
}

func lastEnv(env, key string) string {
	var v string
	for _, l := range strings.Split(env, "\n") {
		if val, ok := strings.CutPrefix(l, key+"="); ok {
			v = val
		}
	}
	return v
}

func TestAnInteractiveSessionOnTheServerCallsThroughTheProxyAsItself(t *testing.T) {
	dir := t.TempDir()
	envFile, argsFile := filepath.Join(dir, "env"), filepath.Join(dir, "args")
	e := proxiedSessions(t, "CLAUDE_FAKE_ENV_FILE="+envFile, "CLAUDE_FAKE_ARGS_FILE="+argsFile)
	exe, _ := os.Executable()
	host := termhost.New(shell.Direct{}, ptyunix.New(), command.New()).
		WithLaunchers(claudecli.NewLauncher(claude.New(llmkit.Options{Bin: exe, ProxyURL: testProxyURL})))
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	e.svc.Terminals = host
	e.svc.SessionHookURL = func(id string) string { return "http://127.0.0.1:8080/api/interactive_session_hook?session_id=" + id }

	status, started := post(t, e.url+"/api/start_interactive_session",
		`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","runs_on":"server","size":{"cols":80,"rows":24}}`)
	if status != http.StatusCreated {
		t.Fatalf("start = %d %q, want 201", status, started.Code)
	}
	id := started.Session.ID

	env := waitFile(t, envFile)
	if got := lastEnv(env, "ANTHROPIC_BASE_URL"); got != testProxyURL {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want the proxy %q", got, testProxyURL)
	}
	if got := lastEnv(env, "ANTHROPIC_CUSTOM_HEADERS"); !strings.Contains(got, "X-Llm-Attribution: "+id) {
		t.Errorf("ANTHROPIC_CUSTOM_HEADERS = %q, want the session's id %s as attribution", got, id)
	}
	if got := lastEnv(env, "TERM"); got != "xterm-256color" {
		t.Errorf("TERM = %q, want xterm-256color", got)
	}

	args := strings.Split(waitFile(t, argsFile), "\n")
	if v, _ := flagAfter(args, "--session-id"); v != started.Session.ClaudeSessionID {
		t.Errorf("--session-id = %q, want the session's conversation %q (args %q)", v, started.Session.ClaudeSessionID, args)
	}
	raw, _ := flagAfter(args, "--settings")
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		t.Fatalf("--settings %q: %v", raw, err)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		if h := settings.Hooks[event]; len(h) == 0 || !strings.Contains(h[0].Hooks[0].Command, "session_id="+id+"&event="+event) {
			t.Errorf("--settings has no %s hook for the session: %s", event, raw)
		}
	}
}

func flagAfter(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}
