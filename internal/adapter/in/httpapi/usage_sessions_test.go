package httpapi

import (
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

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// A headless session through the LLM proxy (seam H1): the claude it starts
// calls through the proxy with its session id as attribution, and its totals
// count the cache tokens the usage records count.

const testProxyURL = "http://127.0.0.1:18080/llm"

func proxiedSessionServer(t *testing.T, env ...string) string {
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
		stubTickets{}, &stubProvisioner{dir: t.TempDir()})
	svc.DefaultEnv = append([]string{"CLAUDE_FAKE=1"}, env...)
	t.Cleanup(func() { mgr.StopAll() })
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: svc}))
	t.Cleanup(srv.Close)
	return srv.URL
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
