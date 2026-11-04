//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// launchSettings is the part of a claude --settings payload this test reads.
type launchSettings struct {
	Attribution *struct {
		Commit *string `json:"commit"`
		PR     *string `json:"pr"`
	} `json:"attribution"`
	Sandbox json.RawMessage `json:"sandbox"`
}

// specSettings is the --settings an interactive launch gets, as claude reads it.
func specSettings(t *testing.T, spec ports.AgentSpec) launchSettings {
	t.Helper()
	var out launchSettings
	raw := map[string]json.RawMessage{}
	for k, v := range spec.Settings {
		raw[k] = v
	}
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestAgentAttribution_ReachesEveryAgentLaunch_EndToEnd runs the real wiring
// with a stand-in claude that records its arguments: the attribution the
// person saves reaches the --settings of the next interactive and headless
// session, beside the hooks and the sandbox key, and a session already
// started keeps what it launched with.
func TestAgentAttribution_ReachesEveryAgentLaunch_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	ctx := context.Background()
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	argsFile := filepath.Join(dir, "claude-args")
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done >> " + argsFile + "\nsleep 5\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var (
		cat   catalog.Catalog
		orch  *orchestration.Service
		store *sandboxsettings.Store
	)
	fxApp := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: bin, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &orch, &store),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })
	if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := cat.Projects.CreateProject(ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := cat.Projects.CreateRepository(ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	orch.Sandbox = &orchestration.Sandbox{Driver: &portstest.RecordingSandboxDriver{}}
	bindSandboxSettings(orch, store)

	save := func(a domain.AgentAttribution) {
		t.Helper()
		if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingAgentAttribution: a.Encode()}); err != nil {
			t.Fatal(err)
		}
	}
	interactive := func() (*domain.Session, ports.AgentSpec) {
		t.Helper()
		s, spec, err := orch.StartInteractive(ctx, ports.InteractiveRequest{ProjectID: p.ID, RepositoryID: repo.ID, Mode: "product", RunsOn: domain.RunnerTUI})
		if err != nil {
			t.Fatalf("start interactive: %v", err)
		}
		return s, spec
	}
	// headless starts a headless session and answers the --settings its claude got.
	headless := func(task string) launchSettings {
		t.Helper()
		_ = os.Remove(argsFile)
		if _, err := orch.Start(ctx, ports.StartRequest{ProjectID: p.ID, RepositoryID: repo.ID, Task: task, AutoAccept: "off"}); err != nil {
			t.Fatalf("start headless: %v", err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			b, _ := os.ReadFile(argsFile)
			lines := strings.Split(string(b), "\n")
			for i, l := range lines {
				if l == "--settings" && i+1 < len(lines) && strings.HasSuffix(lines[i+1], "}") {
					var out launchSettings
					if err := json.Unmarshal([]byte(lines[i+1]), &out); err != nil {
						t.Fatalf("--settings %q: %v", lines[i+1], err)
					}
					return out
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("the headless claude never got --settings")
		return launchSettings{}
	}
	str := func(s *string) string {
		if s == nil {
			return "<absent>"
		}
		return *s
	}

	// Claude default on both: nothing is passed, the other keys are there.
	_, first := interactive()
	if s := specSettings(t, first); s.Attribution != nil || s.Sandbox == nil {
		t.Fatalf("default interactive settings: %+v", first.Settings)
	}
	if s := headless("default"); s.Attribution != nil || s.Sandbox == nil {
		t.Fatalf("default headless settings carry attribution or lack the sandbox key: %+v", s)
	}

	// Commits None, Pull requests Custom text.
	save(domain.AgentAttribution{
		Commit: domain.AttributionChoice{Mode: domain.AttributionNone},
		PR:     domain.AttributionChoice{Mode: domain.AttributionCustom, Text: "Made with care"},
	})
	sess, second := interactive()
	s := specSettings(t, second)
	if s.Attribution == nil || str(s.Attribution.Commit) != "" || str(s.Attribution.PR) != "Made with care" {
		t.Fatalf("interactive attribution = %+v, want commit \"\" and pr \"Made with care\"", s.Attribution)
	}
	if s.Sandbox == nil {
		t.Errorf("the sandbox key was lost beside attribution: %+v", second.Settings)
	}
	if s := specSettings(t, first); s.Attribution != nil {
		t.Errorf("the running session's settings changed: %+v", first.Settings)
	}
	h := headless("changed")
	if h.Attribution == nil || str(h.Attribution.Commit) != "" || str(h.Attribution.PR) != "Made with care" || h.Sandbox == nil {
		t.Fatalf("headless settings = %+v", h)
	}

	// Only one value changed: only that field is passed.
	save(domain.AgentAttribution{Commit: domain.AttributionChoice{Mode: domain.AttributionNone}})
	if s := headless("one value"); s.Attribution == nil || str(s.Attribution.Commit) != "" || s.Attribution.PR != nil {
		t.Fatalf("one-value headless attribution = %+v", s.Attribution)
	}

	// A resume after a change gets the new value, with the sandbox off too.
	save(domain.AgentAttribution{PR: domain.AttributionChoice{Mode: domain.AttributionCustom, Text: "  Co-made  "}})
	if err := store.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	orch.Transcripts = transcriptsThatResume{}
	if _, err := orch.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, resumed, err := orch.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID, RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if s := specSettings(t, resumed); s.Attribution == nil || s.Attribution.Commit != nil || str(s.Attribution.PR) != "Co-made" {
		t.Fatalf("resumed attribution = %+v, want only pr \"Co-made\"", s.Attribution)
	}

	// Back to Claude default: the key is gone again, and a value that is
	// empty after trimming is refused without changing what is saved.
	save(domain.AgentAttribution{})
	_, third := interactive()
	if s := specSettings(t, third); s.Attribution != nil {
		t.Fatalf("attribution after reset: %+v", third.Settings)
	}
	bad := domain.AgentAttribution{Commit: domain.AttributionChoice{Mode: domain.AttributionCustom, Text: "   "}}
	if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingAgentAttribution: bad.Encode()}); err == nil {
		t.Fatal("empty custom text was saved")
	}
	if got := domain.ParseAgentAttribution(cat.Settings.Setting(domain.SettingAgentAttribution)); got != (domain.AgentAttribution{}) {
		t.Fatalf("saved attribution after a refusal = %+v", got)
	}
}
