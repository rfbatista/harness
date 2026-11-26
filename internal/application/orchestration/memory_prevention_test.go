//go:build !windows

package orchestration

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// startHeadlessWithBudget starts a headless session whose orçamento is
// budget bytes, with extraEnv in the server's default session env.
func startHeadlessWithBudget(t *testing.T, budget int64, extraEnv ...string) (*domain.Session, fakeLaunch) {
	t.Helper()
	svc, w := newHeadlessShellService(t)
	svc.DefaultEnv = append(svc.DefaultEnv, extraEnv...)
	savers := withTokenSavers(t, svc)
	setGlobalBudget(t, savers, strconv.FormatInt(budget, 10))
	return startHeadless(t, svc, w.task.ID)
}

// workers = max(1, ⌊orçamento ÷ 2 GB⌋); heap = ⌊orçamento ÷ (workers + 1)⌋ MB.
func TestPrevencaoDeMemoriaFollowsTheOrcamentoInTheSessionsEnvironment(t *testing.T) {
	cases := []struct {
		name        string
		budget      int64
		workers, mb string
	}{
		{"4 GB", 4 * gb, "2", "1365"},
		{"8 GB", 8 * gb, "4", "1638"},
		{"6 GB", 6 * gb, "3", "1536"},
		{"2 GB keeps one worker", 2 * gb, "1", "1024"},
		{"1 GB keeps one worker", 1 * gb, "1", "512"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, launch := startHeadlessWithBudget(t, c.budget)
			if got, want := launch.Env["NODE_OPTIONS"], "--max-old-space-size="+c.mb; got != want {
				t.Errorf("NODE_OPTIONS = %q, want %q", got, want)
			}
			for _, name := range []string{"HARNESS_TEST_WORKERS", "VITEST_MAX_WORKERS", "VITEST_MAX_THREADS", "VITEST_MAX_FORKS"} {
				if launch.Env[name] != c.workers {
					t.Errorf("%s = %q, want %q", name, launch.Env[name], c.workers)
				}
			}
		})
	}
}

// A repository that already sets NODE_OPTIONS keeps its options; the ceiling
// is added only when they carry no --max-old-space-size.
func TestAnExistingNodeOptionsIsKeptAndTheCeilingAddedOnlyWithoutOne(t *testing.T) {
	_, launch := startHeadlessWithBudget(t, 4*gb, "NODE_OPTIONS=--trace-warnings")
	if got, want := launch.Env["NODE_OPTIONS"], "--trace-warnings --max-old-space-size=1365"; got != want {
		t.Errorf("NODE_OPTIONS = %q, want %q", got, want)
	}
	_, launch = startHeadlessWithBudget(t, 4*gb, "NODE_OPTIONS=--trace-warnings --max-old-space-size=512")
	if got, want := launch.Env["NODE_OPTIONS"], "--trace-warnings --max-old-space-size=512"; got != want {
		t.Errorf("NODE_OPTIONS = %q, want the repository's own ceiling kept: %q", got, want)
	}
}

// The brief names the worker count and tells the agent to pass it to Jest and Vitest.
func TestTheBriefTellsTheAgentToPassMaxWorkersToJestAndVitest(t *testing.T) {
	_, launch := startHeadlessWithBudget(t, 4*gb)
	system, _ := flag(launch.Args, "--append-system-prompt")
	for _, want := range []string{"--maxWorkers=$HARNESS_TEST_WORKERS", "Jest", "Vitest"} {
		if !strings.Contains(system, want) {
			t.Errorf("the brief lacks %q:\n%s", want, system)
		}
	}
}

// An interactive session gets the same environment, and a resume moves it to
// the orçamento then in force.
func TestAResumedSessionsPrevencaoFollowsItsNewOrcamento(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	w := withShells(t, svc)
	savers := withTokenSavers(t, svc)
	ctx := context.Background()

	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	env := specEnv(launch.Spec)
	if env["NODE_OPTIONS"] != "--max-old-space-size=1365" || env["HARNESS_TEST_WORKERS"] != "2" {
		t.Errorf("interactive env = NODE_OPTIONS %q, workers %q; want 1365 MB and 2", env["NODE_OPTIONS"], env["HARNESS_TEST_WORKERS"])
	}
	if !strings.Contains(launch.Spec.AppendSystem, "--maxWorkers=$HARNESS_TEST_WORKERS") {
		t.Errorf("interactive brief lacks the maxWorkers line")
	}
	setGlobalBudget(t, savers, strconv.FormatInt(8*gb, 10))
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.EndInteractive(ctx, sess.ID, 0, false) })
	env = specEnv(spec)
	if env["NODE_OPTIONS"] != "--max-old-space-size=1638" || env["HARNESS_TEST_WORKERS"] != "4" {
		t.Errorf("resumed env = NODE_OPTIONS %q, workers %q; want 1638 MB and 4", env["NODE_OPTIONS"], env["HARNESS_TEST_WORKERS"])
	}
}
