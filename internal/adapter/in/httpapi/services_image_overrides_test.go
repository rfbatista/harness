package httpapi

import (
	"os"
	"strings"
	"testing"
)

// Run overrides on an image service (ADR 0077): they travel in the same 0600
// env file as the service's other values, never in argv or the runtime's own
// environment.

const imageOverrideValue = "uniquely-image-override"

func newImageOverridesFixture(t *testing.T) (*overridesFixture, string) {
	t.Helper()
	f := newOverridesFixture(t)
	in := map[string]any{
		"repository_id": f.repoID, "export_repository_env": true,
		"image": map[string]any{"ref": "postgres:16"},
		"env":   "B=b-service\nS=" + secretService,
	}
	in["name"], in["kind"] = "db", "image"
	return f, f.createID(in)
}

func TestServicesImageOverrides_GoThroughTheEnvFileAndNeverArgvOrTheRuntimeEnvironment(t *testing.T) {
	f, id := newImageOverridesFixture(t)
	agent := f.as("sess-1")

	out := agent.ok("start_service", map[string]any{"name": "db", "env": map[string]any{"B": imageOverrideValue, "PGOPT": "x"}})
	run := viewRunOf(out)
	if run["overrides"].(map[string]any)["B"] != imageOverrideValue || run["override_by"] != "session sess-1 (dev)" || run["where"] != "container" {
		t.Fatalf("run = %v", run)
	}
	f.awaitStatus(id, "running")
	runs := f.fake.Runs()
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
	r := runs[0]
	lines := strings.Split(strings.TrimSpace(r.EnvFileAtStart), "\n")
	want := map[string]bool{"A=a-dotenv": true, "B=" + imageOverrideValue: true, "PGOPT=x": true, "C=" + secretDotenv: true, "S=" + secretService: true}
	if len(lines) != len(want) {
		t.Fatalf("env file = %q", r.EnvFileAtStart)
	}
	for _, l := range lines {
		if !want[l] {
			t.Fatalf("env file line %q is unexpected: %q", l, r.EnvFileAtStart)
		}
	}
	if r.EnvFileMode != 0o600 {
		t.Fatalf("env file mode = %v", r.EnvFileMode)
	}
	for _, a := range append(append([]string{}, r.Argv...), r.Env...) {
		if strings.Contains(a, imageOverrideValue) || strings.Contains(a, "PGOPT") {
			t.Fatalf("an override reached argv or the runtime's environment: %q", a)
		}
	}

	// The variables and the agent's view read like a command run's.
	var found bool
	for _, v := range f.variables(id) {
		if v["name"] == "B" {
			found = v["value"] == imageOverrideValue && v["source"] == "override" && v["by"] == "dev · session sess-1"
		}
		if v["name"] == "A" && v["source"] != "repository_env" {
			t.Fatalf("A = %v", v)
		}
	}
	if !found {
		t.Fatalf("variables = %v", f.variables(id))
	}

	f.operate("stop_service", id)
	eventually(t, "the env file to be removed", func() bool { _, err := os.Stat(r.EnvFile); return os.IsNotExist(err) })
}

func TestServicesImageOverrides_ALineBreakInAValueIsRefusedForAnImageButNotACommand(t *testing.T) {
	f, id := newImageOverridesFixture(t)
	agent := f.as("sess-1")

	for _, v := range []string{"a\nb", "a\rb"} {
		_, msg := agent.call("start_service", map[string]any{"name": "db", "env": map[string]any{"MULTI": v}})
		if !strings.Contains(msg, `"MULTI"`) || strings.Contains(msg, "a\nb") {
			t.Fatalf("refusal = %q, want it to name the key and quote no value", msg)
		}
	}
	if st := f.status(id); st != "stopped" || len(f.fake.Runs()) != 0 {
		t.Fatalf("nothing starts: %s, %d runs", st, len(f.fake.Runs()))
	}

	got := agent.ok("start_service", map[string]any{"name": "web", "env": map[string]any{"MULTI": "a\nb"}})
	if viewRunOf(got)["overrides"].(map[string]any)["MULTI"] != "a\nb" {
		t.Fatalf("a command service takes a line break: %v", got)
	}
}

func TestServicesImageOverrides_ARestartRepeatsReplacesAndClearsThemAndOwnStaysRefused(t *testing.T) {
	f, id := newImageOverridesFixture(t)
	agent := f.as("sess-1")
	agent.ok("start_service", map[string]any{"name": "db", "env": map[string]any{"PGOPT": "first"}})
	f.awaitStatus(id, "running")

	repeated := viewRunOf(agent.ok("restart_service", named("db")))
	if repeated["overrides"].(map[string]any)["PGOPT"] != "first" {
		t.Fatalf("a bare restart repeats: %v", repeated)
	}
	f.awaitStatus(id, "running")
	replaced := viewRunOf(agent.ok("restart_service", map[string]any{"name": "db", "env": map[string]any{"PGOPT": "second"}}))
	if replaced["overrides"].(map[string]any)["PGOPT"] != "second" {
		t.Fatalf("replaced = %v", replaced)
	}
	f.awaitStatus(id, "running")
	cleared := viewRunOf(agent.ok("restart_service", map[string]any{"name": "db", "env": map[string]any{}}))
	if len(cleared["overrides"].(map[string]any)) != 0 || cleared["override_by"] != "" {
		t.Fatalf("cleared = %v", cleared)
	}
	f.awaitStatus(id, "running")

	runs := f.fake.Runs()
	if len(runs) != 4 {
		t.Fatalf("runs = %d", len(runs))
	}
	for i, wantLine := range []string{"PGOPT=first", "PGOPT=first", "PGOPT=second", ""} {
		got := strings.Contains(runs[i].EnvFileAtStart, "PGOPT=")
		if wantLine == "" && got || wantLine != "" && !strings.Contains(runs[i].EnvFileAtStart, wantLine) {
			t.Fatalf("run %d env file = %q, want %q", i, runs[i].EnvFileAtStart, wantLine)
		}
	}
	agent.refused("start_service", map[string]any{"name": "db", "worktree": "own", "env": map[string]any{"A": "b"}},
		`"db" cannot run in your worktree: it is an image service`)
}
