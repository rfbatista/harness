//go:build !windows

package orchestration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/capabilities"
	"operators-mcp/internal/application/settings"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// saverWorld is the capabilities context a session's token savers come
// from: activations and installations stored as the pages store them, the
// installed files under a temp data dir.
type saverWorld struct {
	activations   *sqlite.ActivationRepository
	installations *sqlite.TokenSaverInstallationRepository
	settings      *settings.Service
	tools         string
}

// withTokenSavers gives svc a capabilities service to ask for its sessions'
// token savers.
func withTokenSavers(t *testing.T, svc *Service) *saverWorld {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	w := &saverWorld{
		activations:   sqlite.NewActivationRepository(db),
		installations: sqlite.NewTokenSaverInstallationRepository(db),
		settings:      settings.NewService(sqlite.NewSettingsRepository(db), nil),
		tools:         filepath.Join(t.TempDir(), "data dir", "tools"),
	}
	caps := capabilities.NewService(capabilities.Deps{Activations: w.activations, Settings: w.settings})
	caps.UseTokenSavers(capabilities.TokenSaverDeps{Installations: w.installations, Dir: w.tools})
	svc.TokenSavers = caps
	return w
}

// install puts a stub executable where an installed saver lives and
// records its installation; it answers the executable's path.
func (w *saverWorld) install(t *testing.T, saver domain.TokenSaver, name string) string {
	t.Helper()
	dir := filepath.Join(w.tools, string(saver), "v0.0.0-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := w.installations.Put(&domain.TokenSaverInstallation{Saver: saver, Version: "v0.0.0-test", Dir: dir, Path: path, InstalledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return path
}

// uninstall removes an installed saver's files and its installation, as
// Remove on the Token savers page does.
func (w *saverWorld) uninstall(t *testing.T, saver domain.TokenSaver) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(w.tools, string(saver))); err != nil {
		t.Fatal(err)
	}
	if err := w.installations.Delete(saver); err != nil {
		t.Fatal(err)
	}
}

// installRuleset puts a stub caveman SKILL.md where caveman installs and
// records its installation.
func (w *saverWorld) installRuleset(t *testing.T, text string) {
	t.Helper()
	dir := filepath.Join(w.tools, string(domain.TokenSaverCaveman), "v0.0.0-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.installations.Put(&domain.TokenSaverInstallation{Saver: domain.TokenSaverCaveman, Version: "v0.0.0-test", Dir: dir, Path: path, InstalledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func (w *saverWorld) setIntensity(t *testing.T, i domain.CavemanIntensity) {
	t.Helper()
	if _, err := w.settings.UpdateSettings(map[string]string{domain.SettingCavemanIntensity: string(i)}); err != nil {
		t.Fatal(err)
	}
}

func (w *saverWorld) set(t *testing.T, level domain.ActivationLevel, ownerID string, saver domain.TokenSaver, v domain.ActivationValue) {
	t.Helper()
	if err := w.activations.Put(domain.Activation{Saver: saver, Owner: domain.ActivationOwner{Level: level, ID: ownerID}, ProjectID: "p1", Value: v}); err != nil {
		t.Fatal(err)
	}
}

// hookCommands are the commands of event's hook groups for matcher.
func (s claudeSettings) hookCommands(event, matcher string) []string {
	var out []string
	for _, g := range s.Hooks[event] {
		if g.Matcher != matcher {
			continue
		}
		for _, h := range g.Hooks {
			out = append(out, h.Command)
		}
	}
	return out
}

func rtkHook(path string) string { return "'" + path + "' hook claude" }

// checkRTKWired checks a session's settings carry rtk's Bash hook beside
// the edit proxy's, and its env turns rtk's telemetry off without touching
// where its LLM requests go.
func checkRTKWired(t *testing.T, s claudeSettings, env map[string]string, rtk string) {
	t.Helper()
	bash := s.hookCommands("PreToolUse", "Bash")
	if !slices.Contains(bash, rtkHook(rtk)) {
		t.Errorf("PreToolUse Bash hooks %q lack rtk's %q", bash, rtkHook(rtk))
	}
	if !slices.ContainsFunc(bash, func(c string) bool { return strings.Contains(c, "/check?hook=PreToolUse") }) {
		t.Errorf("the edit proxy's Bash hook is gone: %q", bash)
	}
	if _, ok := s.hookCommand("PostToolUse", ShellEditTools); !ok {
		t.Errorf("the edit proxy's commit hook is gone: %+v", s.Hooks)
	}
	if env["RTK_TELEMETRY_DISABLED"] != "1" || env["DO_NOT_TRACK"] != "1" {
		t.Errorf("rtk's telemetry is not off: %v", env)
	}
}

func checkNoSavers(t *testing.T, s claudeSettings, env map[string]string) {
	t.Helper()
	for _, c := range s.hookCommands("PreToolUse", "Bash") {
		if strings.Contains(c, "hook claude") {
			t.Errorf("rtk's hook is in the settings: %q", c)
		}
	}
	if env["RTK_TELEMETRY_DISABLED"] != "" || env["DO_NOT_TRACK"] != "" {
		t.Errorf("rtk's env is set: %v", env)
	}
}

func specEnv(spec ports.AgentSpec) map[string]string {
	out := map[string]string{}
	for _, kv := range spec.Env {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

// startHeadless starts a headless session on the shell world's task and
// answers what its CLI was launched with.
func startHeadless(t *testing.T, svc *Service, taskID string) (*domain.Session, fakeLaunch) {
	t.Helper()
	sess, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: taskID, Task: "go"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), sess.ID) })
	var launch fakeLaunch
	if err := json.Unmarshal(waitFile(t, filepath.Join(sess.WorkingDir, fakeLaunchFile)), &launch); err != nil {
		t.Fatal(err)
	}
	return sess, launch
}

func newHeadlessShellService(t *testing.T) (*Service, *shellWorld) {
	t.Helper()
	svc, _, _ := newTestService(t)
	svc.DefaultEnv = append(svc.DefaultEnv, "CLAUDE_FAKE_RECORD=1")
	w := withShells(t, svc)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	return svc, w
}

// rtk on at project level, installed: the headless session runs with rtk's
// Bash hook beside the edit proxy's and its telemetry off, and records rtk.
func TestAHeadlessSessionRunsWithRTKWhenItIsOnAndInstalled(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	savers := withTokenSavers(t, svc)
	rtk := savers.install(t, domain.TokenSaverRTK, "rtk-oss")
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverRTK, domain.ActivationOn)

	sess, launch := startHeadless(t, svc, w.task.ID)
	checkRTKWired(t, settingsOf(t, launch.Args), launch.Env, rtk)
	if launch.Env["ANTHROPIC_BASE_URL"] != os.Getenv("ANTHROPIC_BASE_URL") {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want it left as it was", launch.Env["ANTHROPIC_BASE_URL"])
	}
	if !slices.Equal(sess.TokenSavers, []domain.TokenSaver{domain.TokenSaverRTK}) || len(sess.TokenSaversNotInstalled) != 0 {
		t.Errorf("session records %v active, %v not installed; want [rtk], none", sess.TokenSavers, sess.TokenSaversNotInstalled)
	}
	got, err := svc.Get(context.Background(), sess.ID)
	if err != nil || !slices.Equal(got.TokenSavers, []domain.TokenSaver{domain.TokenSaverRTK}) {
		t.Errorf("stored session has %v (%v), want [rtk]", got.TokenSavers, err)
	}
}

// A task's off beats its project's on: no hook, nothing recorded.
func TestATasksOffKeepsRTKOutOfItsSessions(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	savers := withTokenSavers(t, svc)
	savers.install(t, domain.TokenSaverRTK, "rtk-oss")
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverRTK, domain.ActivationOn)
	savers.set(t, domain.ActivationLevelTask, w.task.ID, domain.TokenSaverRTK, domain.ActivationOff)

	sess, launch := startHeadless(t, svc, w.task.ID)
	checkNoSavers(t, settingsOf(t, launch.Args), launch.Env)
	if len(sess.TokenSavers) != 0 || len(sess.TokenSaversNotInstalled) != 0 {
		t.Errorf("session records %v active, %v not installed; want none", sess.TokenSavers, sess.TokenSaversNotInstalled)
	}
}

// rtk on but not installed: the session starts without it and records the
// notice.
func TestASessionStartsWithoutAnOnSaverThatIsNotInstalled(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	savers := withTokenSavers(t, svc)
	savers.set(t, domain.ActivationLevelRepository, "r1", domain.TokenSaverRTK, domain.ActivationOn)

	sess, launch := startHeadless(t, svc, w.task.ID)
	checkNoSavers(t, settingsOf(t, launch.Args), launch.Env)
	if len(sess.TokenSavers) != 0 || !slices.Equal(sess.TokenSaversNotInstalled, []domain.TokenSaver{domain.TokenSaverRTK}) {
		t.Errorf("session records %v active, %v not installed; want none, [rtk]", sess.TokenSavers, sess.TokenSaversNotInstalled)
	}
	got, err := svc.Get(context.Background(), sess.ID)
	if err != nil || !slices.Equal(got.TokenSaversNotInstalled, []domain.TokenSaver{domain.TokenSaverRTK}) {
		t.Errorf("stored session has %v not installed (%v), want [rtk]", got.TokenSaversNotInstalled, err)
	}
}

// An interactive session gets rtk the same way, and keeps it when resumed
// after its task turned rtk off: the set is fixed when it starts.
func TestAnInteractiveSessionKeepsTheSaversItStartedWithWhenResumed(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	w := withShells(t, svc)
	savers := withTokenSavers(t, svc)
	rtk := savers.install(t, domain.TokenSaverRTK, "rtk-oss")
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverRTK, domain.ActivationOn)
	ctx := context.Background()

	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	checkRTKWired(t, settingsOf(t, launch.Args), specEnv(launch.Spec), rtk)
	if _, ok := specEnv(launch.Spec)["ANTHROPIC_BASE_URL"]; ok {
		t.Errorf("the session's env sets ANTHROPIC_BASE_URL: %v", launch.Spec.Env)
	}
	if !slices.Equal(sess.TokenSavers, []domain.TokenSaver{domain.TokenSaverRTK}) {
		t.Errorf("session records %v active, want [rtk]", sess.TokenSavers)
	}

	savers.set(t, domain.ActivationLevelTask, w.task.ID, domain.TokenSaverRTK, domain.ActivationOff)
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	resumed, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.EndInteractive(ctx, sess.ID, 0, false) })
	checkRTKWired(t, settingsOf(t, launchOf(t, spec).Args), specEnv(spec), rtk)
	if !slices.Equal(resumed.TokenSavers, []domain.TokenSaver{domain.TokenSaverRTK}) {
		t.Errorf("resumed session records %v active, want [rtk]", resumed.TokenSavers)
	}
}

// A saver of the stored set removed after the session started is left out
// of its resume, which still succeeds, and the session records it as not
// installed, like one that was never installed.
func TestAResumedSessionRunsWithoutAStoredSaverRemovedSinceItStarted(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	w := withShells(t, svc)
	savers := withTokenSavers(t, svc)
	rtk := savers.install(t, domain.TokenSaverRTK, "rtk-oss")
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverRTK, domain.ActivationOn)
	ctx := context.Background()

	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	checkRTKWired(t, settingsOf(t, launch.Args), specEnv(launch.Spec), rtk)
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	savers.uninstall(t, domain.TokenSaverRTK)

	resumed, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.EndInteractive(ctx, sess.ID, 0, false) })
	checkNoSavers(t, settingsOf(t, launchOf(t, spec).Args), specEnv(spec))
	for _, got := range []*domain.Session{resumed, mustGet(t, svc, sess.ID)} {
		if len(got.TokenSavers) != 0 || !slices.Equal(got.TokenSaversNotInstalled, []domain.TokenSaver{domain.TokenSaverRTK}) {
			t.Errorf("session records %v active, %v not installed; want none, [rtk]", got.TokenSavers, got.TokenSaversNotInstalled)
		}
	}
}

func mustGet(t *testing.T, svc *Service, id string) *domain.Session {
	t.Helper()
	sess, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

// With no saver on, a session gets exactly the config it gets from a
// server that knows no token savers.
func TestASessionWithNoActiveSaverGetsTodaysConfig(t *testing.T) {
	plain, _ := newInteractiveService(t)
	_, want := startInteractive(t, plain, InteractiveRequest{})

	svc, _ := newInteractiveService(t)
	savers := withTokenSavers(t, svc)
	savers.install(t, domain.TokenSaverRTK, "rtk-oss")
	savers.installRuleset(t, stubRuleset)
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverRTK, domain.ActivationOff)
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverCaveman, domain.ActivationOff)
	savers.setIntensity(t, domain.CavemanUltra)
	sess, got := startInteractive(t, svc, InteractiveRequest{})

	if !slices.Equal(got.Spec.Env, want.Spec.Env) {
		t.Errorf("env = %q, want %q", got.Spec.Env, want.Spec.Env)
	}
	if got.Spec.AppendSystem != want.Spec.AppendSystem {
		t.Errorf("appended system prompt = %q, want %q", got.Spec.AppendSystem, want.Spec.AppendSystem)
	}
	if g, w := hooksJSON(t, got.Spec), hooksJSON(t, want.Spec); g != w {
		t.Errorf("hooks = %s, want %s", g, w)
	}
	if len(got.Spec.MCPServers) != len(want.Spec.MCPServers) {
		t.Errorf("MCP servers = %+v, want %+v", got.Spec.MCPServers, want.Spec.MCPServers)
	}
	if len(sess.TokenSavers) != 0 || len(sess.TokenSaversNotInstalled) != 0 {
		t.Errorf("session records %v active, %v not installed; want none", sess.TokenSavers, sess.TokenSaversNotInstalled)
	}
}

func hooksJSON(t *testing.T, spec ports.AgentSpec) string {
	t.Helper()
	b, err := json.Marshal(spec.Hooks)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// stubRuleset is a caveman SKILL.md: front matter, then the rules.
const stubRuleset = "---\nname: caveman\ndescription: talk like caveman\n---\n\n# Caveman\n\nDrop articles. Fragments OK.\n"

// checkCavemanWired checks the appended system prompt carries the ruleset
// without its front matter, then the intensity line when there is one,
// then the person-facing instruction, in that order and once each.
func checkCavemanWired(t *testing.T, system string, i domain.CavemanIntensity) {
	t.Helper()
	if strings.Contains(system, "description: talk like caveman") {
		t.Errorf("the ruleset's front matter is in the system prompt:\n%s", system)
	}
	rules := strings.Index(system, "# Caveman\n\nDrop articles. Fragments OK.")
	person := strings.Index(system, "Person-facing text stays in normal prose")
	if rules < 0 || person < 0 || person < rules || strings.Count(system, "Drop articles.") != 1 {
		t.Fatalf("want the ruleset then the person-facing instruction once each in:\n%s", system)
	}
	instruction := system[person:]
	for _, want := range []string{
		"documents (specs, ADRs, glossary, contracts)", "task comments", "review requests", "notifications",
		"commit messages", "pull request text", "code and code comments", "the replies of an interactive session",
		"ticket documents", "task messages between sessions", "a headless session's replies",
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("the person-facing instruction lacks %q:\n%s", want, instruction)
		}
	}
	lite, ultra := strings.Index(system, cavemanLiteLine), strings.Index(system, cavemanUltraLine)
	switch i {
	case domain.CavemanFull:
		if lite >= 0 || ultra >= 0 {
			t.Errorf("full appends an intensity line:\n%s", system)
		}
	case domain.CavemanLite:
		if lite < rules || lite > person || ultra >= 0 {
			t.Errorf("want the lite line between the ruleset and the instruction:\n%s", system)
		}
	case domain.CavemanUltra:
		if ultra < rules || ultra > person || lite >= 0 {
			t.Errorf("want the ultra line between the ruleset and the instruction:\n%s", system)
		}
	}
}

// caveman on and installed, at each intensity: a headless session's
// appended system prompt carries its ruleset at that intensity and the
// person-facing instruction, and records caveman active. Unset is full.
func TestAHeadlessSessionGetsCavemansRulesetAtTheConfiguredIntensity(t *testing.T) {
	for _, i := range []domain.CavemanIntensity{"", domain.CavemanLite, domain.CavemanFull, domain.CavemanUltra} {
		t.Run(string(i), func(t *testing.T) {
			svc, w := newHeadlessShellService(t)
			savers := withTokenSavers(t, svc)
			savers.installRuleset(t, stubRuleset)
			savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverCaveman, domain.ActivationOn)
			want := domain.CavemanFull
			if i != "" {
				savers.setIntensity(t, i)
				want = i
			}

			sess, launch := startHeadless(t, svc, w.task.ID)
			system, _ := flag(launch.Args, "--append-system-prompt")
			checkCavemanWired(t, system, want)
			if launch.Env["DO_NOT_TRACK"] != "1" {
				t.Errorf("DO_NOT_TRACK = %q, want 1", launch.Env["DO_NOT_TRACK"])
			}
			if !slices.Equal(sess.TokenSavers, []domain.TokenSaver{domain.TokenSaverCaveman}) {
				t.Errorf("session records %v active, want [caveman]", sess.TokenSavers)
			}
		})
	}
}

// An interactive session gets the same, through its spec, and again when
// resumed.
func TestAnInteractiveSessionGetsCavemansRulesetAtTheConfiguredIntensity(t *testing.T) {
	for _, i := range []domain.CavemanIntensity{domain.CavemanLite, domain.CavemanFull, domain.CavemanUltra} {
		t.Run(string(i), func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			savers := withTokenSavers(t, svc)
			savers.installRuleset(t, stubRuleset)
			savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverCaveman, domain.ActivationOn)
			savers.setIntensity(t, i)
			ctx := context.Background()

			sess, launch := startInteractive(t, svc, InteractiveRequest{})
			checkCavemanWired(t, launch.Spec.AppendSystem, i)
			system, _ := flag(launch.Args, "--append-system-prompt")
			checkCavemanWired(t, system, i)
			if !slices.Equal(sess.TokenSavers, []domain.TokenSaver{domain.TokenSaverCaveman}) {
				t.Errorf("session records %v active, want [caveman]", sess.TokenSavers)
			}

			if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
				t.Fatal(err)
			}
			_, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = svc.EndInteractive(ctx, sess.ID, 0, false) })
			checkCavemanWired(t, spec.AppendSystem, i)
		})
	}
}

// The intensity is fixed when the session starts, with its savers: a
// resume after the setting changed keeps the one it started with.
func TestAResumedSessionKeepsTheCavemanIntensityItStartedWith(t *testing.T) {
	svc, _ := newInteractiveService(t)
	savers := withTokenSavers(t, svc)
	savers.installRuleset(t, stubRuleset)
	savers.set(t, domain.ActivationLevelProject, "p1", domain.TokenSaverCaveman, domain.ActivationOn)
	savers.setIntensity(t, domain.CavemanLite)
	ctx := context.Background()

	sess, launch := startInteractive(t, svc, InteractiveRequest{})
	checkCavemanWired(t, launch.Spec.AppendSystem, domain.CavemanLite)
	savers.setIntensity(t, domain.CavemanUltra)
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.EndInteractive(ctx, sess.ID, 0, false) })
	checkCavemanWired(t, spec.AppendSystem, domain.CavemanLite)
}
