//go:build darwin

package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/out/agents/claudecli"
	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/ports"
)

// fakeAgentScript is a stand-in claude: it does what an agent's tools do and
// writes what happened to $E2E_RESULT.
const fakeAgentScript = `#!/bin/sh
r="$E2E_RESULT"
: > "$r"
echo "config=$CLAUDE_CONFIG_DIR" >> "$r"
echo "autoupdater=$DISABLE_AUTOUPDATER" >> "$r"
echo "sed-is=$(command -v sed)" >> "$r"
cd "$HARNESS_SHELL" && sed %s -e 's/draft/final/' task/docs/plan.md
echo "shim-edit=$?" >> "$r"
cd "$E2E_WORKTREE" && echo hi > committed.txt && git add committed.txt && git -c user.email=agent@e2e -c user.name=agent commit -q -m "e2e commit"
echo "git-commit=$?" >> "$r"
code=$(curl -s -o /dev/null -w '%%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"ping"}' "$E2E_MCP_URL")
echo "mcp-code=$code" >> "$r"
cat "$E2E_SECRET" >> "$r" 2>/dev/null
echo "secret-read=$?" >> "$r"
`

// TestAgentSandbox_ASessionRunsItsToolsUnderTheRealDriver_EndToEnd launches a
// stand-in claude through the real launcher, llmkit's interactive mode and the
// host's real sandbox driver, with the policy the orchestration built for a
// task session, and checks what the agent still can do and what it cannot.
func TestAgentSandbox_ASessionRunsItsToolsUnderTheRealDriver_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not available")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is required")
	}
	bin := buildServer(t)
	e := newShellE2E(t)
	e.orch.ShimBinary = bin
	e.orch.SessionShellURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/shell" }
	taskID := e.task("Sandboxed")
	if e.orch.Sandbox == nil || e.orch.Sandbox.Driver == nil {
		t.Fatal("the app wired no sandbox driver")
	}

	_, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: taskID, Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Sandbox == nil {
		t.Fatal("the session has no sandbox policy")
	}
	shellDir := envValue(spec.Env, "HARNESS_SHELL")
	doc := e.createDocument("Plan", "# Plan\n\ndraft\n", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskID, "document_id": doc.ID}, nil)
	eventually(t, "the plan in the shell", func() error { return hasFile(shellDir, "task/docs/plan.md", "# Plan\n\ndraft\n", doc.ID, "task", "") })

	// A stand-in secret under the person's real home, which the sandbox
	// must keep from the agent.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	secretDir, err := os.MkdirTemp(home, ".harness-e2e-secret-")
	if err != nil {
		t.Skipf("cannot make a stand-in secret under the home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(secretDir) })
	secret := filepath.Join(secretDir, "token")
	if err := os.WriteFile(secret, []byte("STAND-IN-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(secret); err != nil || string(b) != "STAND-IN-SECRET" {
		t.Fatalf("the secret is not readable unconfined: %v", err)
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte(strings.Replace(fakeAgentScript, "%s", inPlace(), 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	agentHome := claudehome.NewHome(filepath.Join(dir, "claude-home"))
	result := filepath.Join(dir, "result.txt")
	spec.Env = append(spec.Env,
		"E2E_RESULT="+result, "E2E_WORKTREE="+spec.Dir, "E2E_SECRET="+secret,
		"E2E_MCP_URL="+e.srv.URL+mcpsession.PathPrefix+spec.SessionID,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")

	launcher := claudecli.NewLauncher(claude.New(llmkit.Options{Bin: script, ConfigDir: agentHome.Path(), Isolated: true})).
		WithHome(agentHome).
		WithSandbox(e.orch.Sandbox.Driver, ports.SandboxPath{Path: agentHome.Path(), Write: true})
	proc, err := launcher.Launch(context.Background(), spec, ports.TermSize{Cols: 80, Rows: 24}, shell.Direct{})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = readAll(proc) }()
	if code, err := proc.Wait(); err != nil || code != 0 {
		t.Fatalf("the agent exited %d, %v", code, err)
	}
	_ = proc.Close()

	b, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"config=" + agentHome.Path(), "autoupdater=1", "shim-edit=0", "git-commit=0",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("the agent's report lacks %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "sed-is="+envValue(spec.Env, ports.ShimDirEnv)) {
		t.Errorf("sed did not resolve to the PATH shim:\n%s", got)
	}
	if strings.Contains(got, "mcp-code=000") || !strings.Contains(got, "mcp-code=") {
		t.Errorf("the harness MCP endpoint was not reachable over loopback:\n%s", got)
	}
	if strings.Contains(got, "STAND-IN-SECRET") || !strings.Contains(got, "secret-read=1\n") {
		t.Errorf("the agent read a secret under the home:\n%s", got)
	}
	wantDocument(t, e.document(doc.ID), "Plan", "markdown", "task", "# Plan\n\nfinal\n")
	out, err := exec.Command("git", "-C", spec.Dir, "log", "-1", "--format=%s").Output()
	if err != nil || strings.TrimSpace(string(out)) != "e2e commit" {
		t.Errorf("the agent's commit is not in the worktree: %q %v", out, err)
	}

	// The fresh agent home was seeded and the worktree trusted before the
	// start, so an interactive claude meets no first-run or trust prompt.
	cfg, err := os.ReadFile(filepath.Join(agentHome.Path(), ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seeded struct {
		Onboarded bool `json:"hasCompletedOnboarding"`
		Projects  map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(cfg, &seeded); err != nil {
		t.Fatal(err)
	}
	if !seeded.Onboarded || !seeded.Projects[spec.Dir].Trusted {
		t.Errorf("seeded config = %s, want onboarding done and %s trusted", cfg, spec.Dir)
	}
	t.Run("a real claude shows no prompt in the fresh home", func(t *testing.T) {
		t.Skip("needs a real claude and an agent credential: the seeded config above is what it reads")
	})
}

func envValue(env []string, key string) string {
	var v string
	for _, kv := range env {
		if x, ok := strings.CutPrefix(kv, key+"="); ok {
			v = x
		}
	}
	return v
}

func readAll(r interface{ Read([]byte) (int, error) }) (int, error) {
	buf := make([]byte, 4096)
	n := 0
	for {
		m, err := r.Read(buf)
		n += m
		if err != nil {
			return n, err
		}
	}
}
