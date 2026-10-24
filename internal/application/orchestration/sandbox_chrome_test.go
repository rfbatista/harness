package orchestration

import (
	"context"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// wantChromeSection is the contract's system prompt section, in a sandboxed
// session only.
const wantChromeSection = "You run inside the agent sandbox (`HARNESS_SANDBOX` names its driver). Start Chrome or Chromium with `--no-sandbox`: its own sandbox cannot nest inside this one. `MAC_CHROMIUM_TMPDIR` is already set to your temp dir; keep Chrome's `--user-data-dir` under `$TMPDIR` too. Prefer the `mcp__browser__*` tools to drive a browser."

// lastEnvValue is the value a process would see: the last entry wins.
func lastEnvValue(env []string, name string) (string, bool) {
	var v string
	var ok bool
	for _, e := range env {
		if x, found := strings.CutPrefix(e, name+"="); found {
			v, ok = x, true
		}
	}
	return v, ok
}

func chromeWorld(t *testing.T, driver string) *sandboxWorld {
	t.Helper()
	w := tempWorld(t)
	w.driver.DriverName = driver
	t.Setenv("HARNESS_SANDBOX", "leaked")
	t.Setenv("MAC_CHROMIUM_TMPDIR", "/leaked")
	return w
}

func TestSandboxChrome_ASeatbeltSessionGetsTheMarkerAndChromiumsTempDir(t *testing.T) {
	t.Run("headless", func(t *testing.T) {
		w := chromeWorld(t, "seatbelt")
		w.isolate(t)
		if _, err := w.startHeadless(t); err != nil {
			t.Fatal(err)
		}
		env := w.driver.Calls()[0].Command.Env
		if v, _ := lastEnvValue(env, "HARNESS_SANDBOX"); v != "seatbelt" {
			t.Errorf("HARNESS_SANDBOX=%q, want seatbelt", v)
		}
		tmp, _ := lastEnvValue(env, "TMPDIR")
		if v, _ := lastEnvValue(env, "MAC_CHROMIUM_TMPDIR"); v != tmp || tmp == "" {
			t.Errorf("MAC_CHROMIUM_TMPDIR=%q, want TMPDIR %q", v, tmp)
		}
	})
	t.Run("interactive", func(t *testing.T) {
		w := chromeWorld(t, "seatbelt")
		_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := lastEnvValue(spec.Env, "HARNESS_SANDBOX"); v != "seatbelt" {
			t.Errorf("HARNESS_SANDBOX=%q, want seatbelt", v)
		}
		tmp, _ := lastEnvValue(spec.Env, "TMPDIR")
		if v, _ := lastEnvValue(spec.Env, "MAC_CHROMIUM_TMPDIR"); v != tmp || tmp == "" {
			t.Errorf("MAC_CHROMIUM_TMPDIR=%q, want TMPDIR %q", v, tmp)
		}
	})
	t.Run("resume", func(t *testing.T) {
		w := chromeWorld(t, "seatbelt")
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
		tmp, _ := lastEnvValue(spec.Env, "TMPDIR")
		if v, _ := lastEnvValue(spec.Env, "MAC_CHROMIUM_TMPDIR"); v != tmp || tmp == "" {
			t.Errorf("MAC_CHROMIUM_TMPDIR=%q, want TMPDIR %q", v, tmp)
		}
	})
}

func TestSandboxChrome_ABubblewrapSessionGetsOnlyTheMarker(t *testing.T) {
	w := chromeWorld(t, "bubblewrap")
	w.isolate(t)
	if _, err := w.startHeadless(t); err != nil {
		t.Fatal(err)
	}
	env := w.driver.Calls()[0].Command.Env
	if v, _ := lastEnvValue(env, "HARNESS_SANDBOX"); v != "bubblewrap" {
		t.Errorf("HARNESS_SANDBOX=%q, want bubblewrap", v)
	}
	if v, ok := lastEnvValue(env, "MAC_CHROMIUM_TMPDIR"); ok {
		t.Errorf("MAC_CHROMIUM_TMPDIR=%q set under bubblewrap", v)
	}
}

func TestSandboxChrome_TheMarkerDoesNotNeedASessionTempDir(t *testing.T) {
	w := chromeWorld(t, "seatbelt")
	w.svc.Sandbox.TempDir = nil
	_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := lastEnvValue(spec.Env, "HARNESS_SANDBOX"); v != "seatbelt" {
		t.Errorf("HARNESS_SANDBOX=%q, want seatbelt", v)
	}
	if v, ok := lastEnvValue(spec.Env, "MAC_CHROMIUM_TMPDIR"); ok {
		t.Errorf("MAC_CHROMIUM_TMPDIR=%q without a temp dir", v)
	}
}

func TestSandboxChrome_AnUnsandboxedSessionHasNeitherVariableEvenFromTheServerEnv(t *testing.T) {
	w := chromeWorld(t, "seatbelt")
	w.off = true
	in := prepareInput{ProjectID: "p1", RepositoryID: "r1", Branch: "agent/chrome", Headless: true}
	p, err := w.svc.prepare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if p.cfg.Wrap == nil {
		t.Fatal("no wrap: the server env would reach the session")
	}
	leaky := []string{"A=1", "HARNESS_SANDBOX=leaked", "MAC_CHROMIUM_TMPDIR=/leaked"}
	out, err := p.cfg.Wrap(llmkit.Command{Env: leaky})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HARNESS_SANDBOX", "MAC_CHROMIUM_TMPDIR"} {
		for _, e := range out.Env {
			if strings.HasPrefix(e, name+"=") {
				t.Errorf("env has %q, want no %s entry at all", e, name)
			}
		}
	}
	if len(out.Env) != 1 || out.Env[0] != "A=1" {
		t.Errorf("env %v, want only A=1", out.Env)
	}
}

func TestSandboxChrome_AnUnsandboxedInteractiveSessionDeclaresNeither(t *testing.T) {
	w := chromeWorld(t, "seatbelt")
	w.off = true
	_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HARNESS_SANDBOX", "MAC_CHROMIUM_TMPDIR"} {
		if v, ok := lastEnvValue(spec.Env, name); ok {
			t.Errorf("%s=%q declared by an unsandboxed session", name, v)
		}
	}
}

func TestSandboxChrome_OnlyASandboxedSessionsPromptHasTheSection(t *testing.T) {
	w := chromeWorld(t, "seatbelt")
	_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec.AppendSystem, wantChromeSection) {
		t.Errorf("sandboxed prompt lacks the Chrome section:\n%s", spec.AppendSystem)
	}
	w.off = true
	_, spec, err = w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spec.AppendSystem, "--no-sandbox") {
		t.Errorf("unsandboxed prompt has the Chrome section:\n%s", spec.AppendSystem)
	}
}
