package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// scriptedJudge answers the judge's two questions with fixed JSON; the model
// is never called.
type scriptedJudge struct{ models []string }

func (j *scriptedJudge) Ask(_ context.Context, model, system, _ string) (string, error) {
	j.models = append(j.models, model)
	if strings.Contains(system, "similarity") {
		return `{"similarity": 0.9}`, nil
	}
	return `{"score": 0.8, "steps": [{"step": 1, "ok": true, "note": "fine"}]}`, nil
}

type effectiveCritic string

func (m effectiveCritic) Effective(context.Context, string, string) (domain.EffectiveAgentSystem, error) {
	var e domain.EffectiveAgentSystem
	e.Review.Critic.Model.Value = string(m)
	return e, nil
}

type benchFeed struct {
	mu      sync.Mutex
	changes []ports.BenchRunChange
}

func (f *benchFeed) AnnounceChange(c ports.ProjectChange) {
	if c.BenchRun != nil {
		f.mu.Lock()
		f.changes = append(f.changes, *c.BenchRun)
		f.mu.Unlock()
	}
}

func (f *benchFeed) all() []ports.BenchRunChange {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.BenchRunChange(nil), f.changes...)
}

// startedWorld is a benchWorld with its cases stored and a scorecard store,
// a scripted judge and a feed.
type runWorld struct {
	*benchWorld
	cases []domain.BenchCase
	judge *scriptedJudge
	feed  *benchFeed
}

const readScript = `[{"tool":"read_file","params":{"path":"README.md"}}]`

func newRunWorld(t *testing.T, script string, prompts ...string) *runWorld {
	t.Helper()
	return newRunWorldWithin(t, 0, script, prompts...)
}

// newRunWorldWithin is newRunWorld with a time limit per case; zero is the
// service's own.
func newRunWorldWithin(t *testing.T, caseTimeout time.Duration, script string, prompts ...string) *runWorld {
	t.Helper()
	w := newBenchWorld(t, script)
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	rw := &runWorld{benchWorld: w, judge: &scriptedJudge{}, feed: &benchFeed{}}
	rw.bench = bench.New(bench.Deps{
		Cases:      sqlite.NewBenchCaseRepository(db),
		Scorecards: sqlite.NewBenchScorecardRepository(db),
		Projects:   benchProjects{w.sessions},
		Sessions:   w.sessions,
		ServeMock:  w.serveMock,
		MockName:   "bench",
		Judge:      rw.judge,
		Settings:   effectiveCritic("opus-critic"),
		Feed:       rw.feed,

		CaseTimeout: caseTimeout,
	})
	if len(prompts) == 0 {
		prompts = []string{"Read the README.", "Read the README again."}
	}
	for _, p := range prompts {
		c, err := rw.bench.CreateCase(context.Background(), "p1", domain.BenchCase{
			Prompt: p,
			Tools:  []domain.BenchTool{{Name: "read_file", Result: "# Demo"}},
			Expect: domain.BenchExpectation{
				Tools:     []domain.BenchExpectedCall{{Tool: "read_file", Params: map[string]any{"path": "README.md"}}},
				Ordered:   true,
				Reference: "A demo project.",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		rw.cases = append(rw.cases, c)
	}
	return rw
}

type benchProjects struct{ svc *Service }

func (p benchProjects) Get(ctx context.Context, id string) *domain.Project {
	proj, err := p.svc.catalog.Projects.GetProject(ctx, id)
	if err != nil {
		return nil
	}
	return proj
}

// startRun starts a run and waits for it to end.
func (w *runWorld) startRun(t *testing.T) domain.BenchRun {
	t.Helper()
	run, err := w.bench.StartRun(context.Background(), "p1", domain.BenchRunRequest{AgentID: "dev", RepositoryID: "r1", Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	return w.waitEnd(t, run.ID)
}

func (w *runWorld) waitEnd(t *testing.T, id string) domain.BenchRun {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		run, err := w.bench.GetRun(context.Background(), "p1", id)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != domain.BenchRunRunning {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the run did not end")
	return domain.BenchRun{}
}

func (w *runWorld) scorecard(t *testing.T, run domain.BenchRun) domain.Scorecard {
	t.Helper()
	if run.ScorecardID == "" {
		t.Fatalf("run %+v has no scorecard", run)
	}
	card, err := w.bench.GetScorecard(context.Background(), "p1", run.ScorecardID)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func TestABenchRunProducesAStoredScorecardWithScoresCostAndSetup(t *testing.T) {
	w := newRunWorld(t, readScript)
	run := w.startRun(t)
	if run.Status != domain.BenchRunFinished || run.Done != 2 || run.Total != 2 {
		t.Fatalf("run = %+v", run)
	}

	card := w.scorecard(t, run)
	if card.Status != domain.ScorecardFinished || len(card.Cases) != 2 {
		t.Fatalf("scorecard = %+v", card)
	}
	for _, c := range card.Cases {
		if c.ToolUse.RightTool.Score != 1 || c.ToolUse.ValidParams.Score != 1 || c.ToolUse.RightOrder.Score != 1 {
			t.Fatalf("tool use = %+v", c.ToolUse)
		}
		if !c.Outcome.Scored || c.Outcome.Score != 0.9 || !c.Trajectory.Scored || len(c.Steps) != 1 || len(c.Calls) != 1 {
			t.Fatalf("case = %+v", c)
		}
	}
	if card.Aggregate.Scored != 2 || card.Patterns[domain.BenchPatternRightTool].Mean != 1 {
		t.Fatalf("aggregate %+v patterns %+v", card.Aggregate, card.Patterns)
	}
	if card.CostUSD == 0 || card.Setup.Fingerprint == "" || card.Setup.AgentName != "go-developer" || card.Setup.CLIVersion != "2.1.0" {
		t.Fatalf("cost %v setup %+v", card.CostUSD, card.Setup)
	}
	// The judge ran on the project's effective critic model, shown on the scorecard.
	if card.Setup.JudgeModel != "opus-critic" {
		t.Fatalf("judge model = %q", card.Setup.JudgeModel)
	}
	for _, m := range w.judge.models {
		if m != "opus-critic" {
			t.Fatalf("the judge ran on %q", m)
		}
	}
}

func TestARepeatOnAnUnchangedSetupIsComparableAndAChangedPromptShowsItsDifferencePerCase(t *testing.T) {
	w := newRunWorld(t, readScript)
	first := w.scorecard(t, w.startRun(t))
	second := w.scorecard(t, w.startRun(t))

	cmp, err := w.bench.CompareScorecard(context.Background(), "p1", second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.PreviousID != first.ID || cmp.FingerprintChanged || cmp.Aggregate.Change != 0 {
		t.Fatalf("repeat comparison = %+v", cmp)
	}
	for _, c := range cmp.Cases {
		if c.Status != domain.BenchCaseUnchanged {
			t.Fatalf("case %s is %s on an unchanged setup", c.CaseID, c.Status)
		}
	}

	// A changed prompt: the agent now calls the wrong tool.
	w.sessions.catalog.Projects.(*fakeResolver).agents["dev"].Prompt.Content = "You never read files."
	w.sessions.DefaultEnv = replaceEnv(w.sessions.DefaultEnv, fakeScriptEnv, `[{"tool":"delete_everything","params":{}}]`)
	third := w.scorecard(t, w.startRun(t))
	cmp, err = w.bench.CompareScorecard(context.Background(), "p1", third.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !cmp.FingerprintChanged || cmp.Aggregate.Change >= 0 {
		t.Fatalf("changed comparison = %+v", cmp)
	}
	for _, c := range cmp.Cases {
		if c.Status != domain.BenchCaseChanged || c.Patterns[domain.BenchPatternRightTool].Change >= 0 {
			t.Fatalf("case %+v, want a lower right_tool", c)
		}
	}
}

func replaceEnv(env []string, name, value string) []string {
	out := []string{}
	for _, e := range env {
		if !strings.HasPrefix(e, name+"=") {
			out = append(out, e)
		}
	}
	return append(out, name+"="+value)
}

func TestABenchRunLeavesTheProjectsRepositoryAndSessionsAlone(t *testing.T) {
	w := newRunWorld(t, readScript)
	before := tree(t, w.repoRoot)
	w.startRun(t)
	if after := tree(t, w.repoRoot); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("the repository changed:\nbefore %v\nafter  %v", before, after)
	}
	sessions := w.sessions.sessions.List(context.Background(), ports.SessionFilter{})
	if len(sessions) != 0 {
		t.Fatalf("a bench run started %d sessions on the project", len(sessions))
	}
}

func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil {
			info, _ := d.Info()
			out = append(out, p+" "+info.ModTime().String())
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func TestACancelledRunEndsTheCLIAndKeepsTheScorecardOfTheCasesDone(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "hang.pid")
	w := newRunWorld(t, readScript, "Read the README.", "HANG on this one")
	w.sessions.DefaultEnv = append(w.sessions.DefaultEnv, "CLAUDE_FAKE_PIDFILE="+pid)

	run, err := w.bench.StartRun(context.Background(), "p1", domain.BenchRunRequest{AgentID: "dev", RepositoryID: "r1", Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	var cliPID int
	waitFor(t, "the second case's CLI to hang", func() bool {
		b, err := os.ReadFile(pid)
		if err != nil {
			return false
		}
		cliPID, _ = strconv.Atoi(string(b))
		return cliPID > 0
	})
	if _, err := w.bench.CancelRun(context.Background(), "p1", run.ID); err != nil {
		t.Fatal(err)
	}
	ended := w.waitEnd(t, run.ID)
	if ended.Status != domain.BenchRunCancelled || ended.Done != 1 {
		t.Fatalf("run = %+v", ended)
	}
	card := w.scorecard(t, ended)
	if card.Status != domain.ScorecardCancelled || len(card.Cases) != 1 {
		t.Fatalf("scorecard = %+v", card)
	}
	waitFor(t, "the CLI to end", func() bool { return syscall.Kill(cliPID, 0) != nil })

	// A run that ended cannot be cancelled again.
	if _, err := w.bench.CancelRun(context.Background(), "p1", run.ID); err == nil {
		t.Fatal("cancelling an ended run was accepted")
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestARunAnnouncesItsProgressPerCaseAndItsEndWithTheScorecard(t *testing.T) {
	w := newRunWorld(t, readScript)
	run := w.startRun(t)
	// The end is announced just after the run reads as ended.
	waitFor(t, "the end to be announced", func() bool {
		all := w.feed.all()
		return len(all) > 0 && all[len(all)-1].Status == domain.BenchRunFinished
	})

	var done []int
	changes := w.feed.all()
	last := changes[len(changes)-1]
	for _, c := range changes {
		if c.RunID != run.ID || c.Total != 2 {
			t.Fatalf("change = %+v", c)
		}
		done = append(done, c.Done)
	}
	if last.Status != domain.BenchRunFinished || last.ScorecardID != run.ScorecardID || last.Done != 2 {
		t.Fatalf("the last change = %+v, want finished with the scorecard", last)
	}
	if done[0] != 1 {
		t.Fatalf("progress = %v, want to start at 1 of 2", done)
	}
}

// A CLI that never answers is stopped at the case's time limit: that case is a
// failure with its reason, the run goes on to the next case and finishes.
func TestACaseThatRunsPastItsTimeLimitFailsAloneAndTheRunFinishes(t *testing.T) {
	w := newRunWorldWithin(t, 1500*time.Millisecond, readScript, "HANG on this one", "Read the README.")
	run := w.startRun(t)
	if run.Status != domain.BenchRunFinished {
		t.Fatalf("run = %+v, want finished", run)
	}
	card := w.scorecard(t, run)
	if len(card.Cases) != 2 || !strings.Contains(card.Cases[0].Error, "time limit") || card.Cases[1].Error != "" {
		t.Fatalf("cases = %+v", card.Cases)
	}
	if card.Aggregate.Scored != 2 || card.Aggregate.Mean >= 1 {
		t.Fatalf("aggregate = %+v, a timed-out case must count against it", card.Aggregate)
	}
}
