package orchestration

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// wantBrowserArgs is the contract's browser server command line (ADR 0025).
var wantBrowserArgs = []string{"-y", "@playwright/mcp@0.0.83", "--headless", "--isolated", "--browser", "chrome", "--no-sandbox"}

func browserServers(servers []llmkit.MCPServerSpec) []llmkit.MCPServerSpec {
	var out []llmkit.MCPServerSpec
	for _, s := range servers {
		if s.Name == "browser" {
			out = append(out, s)
		}
	}
	return out
}

func checkBrowser(t *testing.T, servers []llmkit.MCPServerSpec, allowed []string) llmkit.MCPServerSpec {
	t.Helper()
	got := browserServers(servers)
	if len(got) != 1 {
		t.Fatalf("%d browser servers, want 1: %+v", len(got), servers)
	}
	b := got[0]
	if b.Transport != "stdio" || b.Command != "npx" || !slices.Equal(b.Args, wantBrowserArgs) {
		t.Errorf("browser server = %+v, want npx %v over stdio", b, wantBrowserArgs)
	}
	if !slices.Contains(allowed, "mcp__browser") {
		t.Errorf("allowed tools %v lack mcp__browser", allowed)
	}
	return b
}

func TestBrowser_EverySandboxedSessionGetsTheServer(t *testing.T) {
	t.Run("headless", func(t *testing.T) {
		w := tempWorld(t)
		p, err := w.svc.prepare(context.Background(), prepareInput{ProjectID: "p1", RepositoryID: "r1", Branch: "agent/b", Headless: true})
		if err != nil {
			t.Fatal(err)
		}
		checkBrowser(t, p.cfg.MCPServers, p.cfg.AllowedTools)
	})
	for _, mode := range []string{"implement", "design", "", "architect", "product"} {
		t.Run("interactive "+mode, func(t *testing.T) {
			w := tempWorld(t)
			req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: mode, RunsOn: domain.RunnerTUI}
			if mode != "product" {
				req.TicketID = "tk1"
			}
			_, spec, err := w.svc.StartInteractive(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			var servers []llmkit.MCPServerSpec
			for _, s := range spec.MCPServers {
				servers = append(servers, llmkit.MCPServerSpec{Name: s.Name, Transport: s.Transport, Command: s.Command, Args: s.Args, Env: s.Env})
			}
			checkBrowser(t, servers, spec.AllowedTools)
		})
	}
	t.Run("resume", func(t *testing.T) {
		w := tempWorld(t)
		sess, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.svc.sessions.UpdateStatus(sess.ID, domain.SessionDone); err != nil {
			t.Fatal(err)
		}
		_, spec, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
		if err != nil {
			t.Fatal(err)
		}
		var n int
		for _, s := range spec.MCPServers {
			if s.Name == "browser" {
				n++
			}
		}
		if n != 1 || !slices.Contains(spec.AllowedTools, "mcp__browser") {
			t.Errorf("resume: %d browser servers, allowed %v", n, spec.AllowedTools)
		}
	})
}

func TestBrowser_AnUnsandboxedSessionHasNoServer(t *testing.T) {
	w := tempWorld(t)
	w.off = true
	p, err := w.svc.prepare(context.Background(), prepareInput{ProjectID: "p1", RepositoryID: "r1", Branch: "agent/b", Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := browserServers(p.cfg.MCPServers); len(got) != 0 || slices.Contains(p.cfg.AllowedTools, "mcp__browser") {
		t.Errorf("unsandboxed: servers %+v, allowed %v", got, p.cfg.AllowedTools)
	}
	_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range spec.MCPServers {
		if s.Name == "browser" {
			t.Errorf("unsandboxed interactive session has the browser server")
		}
	}
}

// Playwright keeps its unix socket under PWTEST_SOCKETS_DIR and its registry
// under the home it sees; the server runs with both in the session's temp dir,
// and npm keeps its cache where it was.
func TestBrowser_UnderSeatbeltTheServerRunsInTheSessionsTempDir(t *testing.T) {
	w := chromeWorld(t, "seatbelt")
	w.svc.Sandbox.UserHome = "/Users/someone"
	t.Setenv("npm_config_cache", "")
	p, err := w.svc.prepare(context.Background(), prepareInput{ProjectID: "p1", RepositoryID: "r1", Branch: "agent/b", Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	b := checkBrowser(t, p.cfg.MCPServers, p.cfg.AllowedTools)
	tmp, _ := lastEnvValue(p.cfg.Env, "TMPDIR")
	if tmp == "" {
		t.Fatal("no TMPDIR")
	}
	for k, want := range map[string]string{"PWTEST_SOCKETS_DIR": tmp, "HOME": tmp, "npm_config_cache": "/Users/someone/.npm"} {
		if b.Env[k] != want {
			t.Errorf("browser server %s=%q, want %q", k, b.Env[k], want)
		}
	}

	w = chromeWorld(t, "bubblewrap")
	p, err = w.svc.prepare(context.Background(), prepareInput{ProjectID: "p1", RepositoryID: "r1", Branch: "agent/b", Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	b = checkBrowser(t, p.cfg.MCPServers, p.cfg.AllowedTools)
	for _, k := range []string{"PWTEST_SOCKETS_DIR", "HOME", "npm_config_cache"} {
		if v, ok := b.Env[k]; ok {
			t.Errorf("bubblewrap: browser server has %s=%q", k, v)
		}
	}
}

func TestBrowser_ThePromptSaysToPreferTheTools(t *testing.T) {
	w := tempWorld(t)
	_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec.AppendSystem, wantChromeSection) || !strings.Contains(spec.AppendSystem, "Prefer the `mcp__browser__*` tools to drive a browser.") {
		t.Errorf("prompt lacks the Chrome section and the browser sentence:\n%s", spec.AppendSystem)
	}
}
