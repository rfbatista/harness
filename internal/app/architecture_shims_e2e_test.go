//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The architecture rules in the PATH shims, run as real processes the way an
// agent's Bash tool runs them (contract "Harness server ↔ Agent sessions —
// repository architecture record").

// asWithShims starts a session with no agent (plain), with the shims on its
// PATH and the harness at the test server.
func (h *archHooks) asWithShims(bin string) shimSession {
	h.t.Helper()
	h.orch.SessionArchitectureURL = func(id string) string { return h.srv.URL + "/api/sessions/" + id + "/architecture" }
	return h.startWithShims(h.task, bin)
}

func (s shimSession) inWorktree(t *testing.T, line string) ran {
	t.Helper()
	return sh(t, s.worktree, s.env, "", line)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestABlockingRuleRefusesEachShimmedWriteOutsideTheAgentsLayers(t *testing.T) {
	bin := buildServer(t)
	h := newArchHooks(t, "blocking")
	a := h.asWithShims(bin)
	// The session has no agent: plain reads every layer and changes none.
	const rule = "Architecture rule stay-in-your-layer (blocking): Ask the architect. — "
	domainFile := filepath.Join(a.worktree, "internal", "domain", "zone.go")
	write(t, domainFile, "old\n")
	write(t, filepath.Join(a.worktree, "internal", "domain", "other.go"), "other\n")
	outside := t.TempDir()
	write(t, filepath.Join(outside, "src.txt"), "src\n")

	for name, c := range map[string]struct {
		line, want string
		unchanged  func() bool
	}{
		"sed -i": {"sed " + inPlace() + " 's/old/new/' internal/domain/zone.go", "internal/domain/zone.go is in layer domain, which plain may not change.", func() bool { return read(t, domainFile) == "old\n" }},
		"tee":    {"printf 'new\\n' | tee internal/domain/zone.go", "internal/domain/zone.go is in layer domain", func() bool { return read(t, domainFile) == "old\n" }},
		"cp": {"cp " + filepath.Join(outside, "src.txt") + " internal/domain/copied.go", "internal/domain/copied.go is in layer domain", func() bool {
			return read(t, filepath.Join(a.worktree, "internal", "domain", "copied.go")) == "<missing>"
		}},
		"mv":    {"mv internal/domain/other.go moved.go", "internal/domain/other.go is in layer domain", func() bool { return read(t, filepath.Join(a.worktree, "internal", "domain", "other.go")) == "other\n" }},
		"rm":    {"rm internal/domain/zone.go", "internal/domain/zone.go is in layer domain", func() bool { return read(t, domainFile) == "old\n" }},
		"touch": {"touch internal/domain/new.go", "internal/domain/new.go is in layer domain", func() bool { return read(t, filepath.Join(a.worktree, "internal", "domain", "new.go")) == "<missing>" }},
		"mkdir": {"mkdir internal/domain/sub", "internal/domain/sub is in layer domain", func() bool { return read(t, filepath.Join(a.worktree, "internal", "domain", "sub")) == "<missing>" }},
	} {
		r := a.inWorktree(t, c.line)
		if r.code != 1 || !strings.Contains(r.stderr, rule) || !strings.Contains(r.stderr, c.want) {
			t.Errorf("%s: %+v, want exit 1 with %q", name, r, rule+c.want)
		}
		if !c.unchanged() {
			t.Errorf("%s changed the file", name)
		}
	}
	if events := h.events(); len(events) != 8 || events[0]["at_point"] != "before_tool" || events[0]["outcome"] != "blocked" {
		t.Errorf("events = %d: %v", len(events), events)
	}

	// An mv names two paths, so it left two events (each is a change); outside the worktree nothing is checked.
	if r := a.inWorktree(t, "cp "+filepath.Join(outside, "src.txt")+" "+filepath.Join(outside, "dst.txt")+" && rm "+filepath.Join(outside, "dst.txt")); r.code != 0 {
		t.Errorf("a copy outside the worktree: %+v", r)
	}
	if events := h.events(); len(events) != 8 {
		t.Errorf("a command outside the worktree left an event: %d", len(events))
	}
}

func TestAWarningRuleLetsAShimmedWriteRunAndTellsTheAgent(t *testing.T) {
	bin := buildServer(t)
	h := newArchHooks(t, "warning")
	a := h.asWithShims(bin)
	file := filepath.Join(a.worktree, "internal", "domain", "zone.go")
	write(t, file, "old\n")
	r := a.inWorktree(t, "sed "+inPlace()+" 's/old/new/' internal/domain/zone.go")
	if r.code != 0 || !strings.Contains(r.stderr, "Architecture rule stay-in-your-layer (warning): Ask the architect. — internal/domain/zone.go is in layer domain, which plain may not change.") {
		t.Errorf("%+v", r)
	}
	if read(t, file) != "new\n" {
		t.Error("the warned write did not run")
	}
	if events := h.events(); len(events) != 1 || events[0]["outcome"] != "warned" {
		t.Errorf("events = %v", events)
	}
}

func TestAnUnreachableHarnessLetsAShimmedWriteRunAndSaysSo(t *testing.T) {
	bin := buildServer(t)
	h := newArchHooks(t, "blocking")
	a := h.asWithShims(bin)
	file := filepath.Join(a.worktree, "internal", "domain", "zone.go")
	write(t, file, "old\n")
	down := append(append([]string{}, a.env...), "HARNESS_ARCHITECTURE_URL=http://127.0.0.1:1/api/sessions/x/architecture")
	r := sh(t, a.worktree, down, "", "sed "+inPlace()+" 's/old/new/' internal/domain/zone.go")
	if r.code != 0 || !strings.Contains(r.stderr, "Architecture rules could not be checked: the harness could not be reached.") || read(t, file) != "new\n" {
		t.Errorf("%+v", r)
	}
}
