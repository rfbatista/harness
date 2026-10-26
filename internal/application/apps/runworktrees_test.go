package apps_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/domain"
)

type repos map[string]*domain.Repository

func (r repos) Get(_ context.Context, id string) *domain.Repository { return r[id] }

type agentNames map[string]string

func (a agentNames) Get(id string) *domain.Agent {
	if n, ok := a[id]; ok {
		return &domain.Agent{ID: id, Name: n}
	}
	return nil
}

type branches map[string]string

func (b branches) CurrentBranch(_ context.Context, dir string) (string, error) { return b[dir], nil }

// worktreeFixture is a repository r1 with a main checkout and three sessions:
// s1 (the one asking), s2 and s3 of r1, and x1 of another repository.
type worktreeFixture struct {
	svc                   *apps.Service
	own, main, w2, w3, wx string
}

func newWorktreeFixture(t *testing.T) worktreeFixture {
	t.Helper()
	f := worktreeFixture{own: t.TempDir(), main: t.TempDir(), w2: t.TempDir(), w3: t.TempDir(), wx: t.TempDir()}
	now := time.Now()
	all := sessions{
		"s1":   {ID: "s1", ProjectID: "p1", RepositoryID: "r1", WorkingDir: f.own, Branch: "agent/own", Status: domain.SessionRunning, CreatedAt: now},
		"s2":   {ID: "s2", ProjectID: "p1", RepositoryID: "r1", WorkingDir: f.w2, Branch: "agent/older", AgentID: "a1", Status: domain.SessionDone, CreatedAt: now.Add(-2 * time.Hour)},
		"s3":   {ID: "s3", ProjectID: "p1", RepositoryID: "r1", WorkingDir: f.w3, Branch: "agent/newer", AgentID: "a1", Status: domain.SessionRunning, CreatedAt: now.Add(-time.Hour)},
		"x1":   {ID: "x1", ProjectID: "p1", RepositoryID: "r2", WorkingDir: f.wx, Branch: "agent/other", CreatedAt: now},
		"bare": {ID: "bare", ProjectID: "p1", RepositoryID: "r1", CreatedAt: now},
	}
	run, _ := newRunnerWith(t, all)
	run.UseRunWorktrees(repos{"r1": {ID: "r1", RootDir: f.main}}, agentNames{"a1": "go-developer"}, branches{f.main: "main"})
	f.svc = run
	return f
}

func TestTheRunWorktreesAreOwnThenMainThenOtherSessionsNewestFirst(t *testing.T) {
	f := newWorktreeFixture(t)
	got, err := f.svc.ListRunWorktrees(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.RunWorktree{
		{Key: "own", Kind: domain.RunWorktreeOwn, Branch: "agent/own", Dir: f.own, SessionID: "s1", SessionStatus: domain.SessionRunning},
		{Key: "main", Kind: domain.RunWorktreeMain, Branch: "main", Dir: f.main},
		{Key: "session:s3", Kind: domain.RunWorktreeSession, Branch: "agent/newer", Dir: f.w3, SessionID: "s3", Agent: "go-developer", SessionStatus: domain.SessionRunning},
		{Key: "session:s2", Kind: domain.RunWorktreeSession, Branch: "agent/older", Dir: f.w2, SessionID: "s2", Agent: "go-developer", SessionStatus: domain.SessionDone},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if *got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, *got[i], want[i])
		}
	}
}

func TestAWorktreeThatIsGoneFromDiskIsNotOffered(t *testing.T) {
	f := newWorktreeFixture(t)
	if err := os.RemoveAll(f.w3); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.ListRunWorktrees(context.Background(), "s1")
	for _, w := range got {
		if w.Key == "session:s3" {
			t.Fatalf("a worktree gone from disk is offered: %+v", got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestASessionWithoutAWorktreeHasNoRunWorktrees(t *testing.T) {
	f := newWorktreeFixture(t)
	if _, err := f.svc.ListRunWorktrees(context.Background(), "bare"); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("got %v", err)
	}
	if _, err := f.svc.ListRunWorktrees(context.Background(), "ghost"); errs.Code(err) != "SESSION_NOT_FOUND" {
		t.Fatalf("got %v", err)
	}
}

func TestARunStartsInThePickedRunWorktreeAndStaysWithItsSession(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	for _, c := range []struct {
		key  string
		dir  string
		want domain.RunWorktreeRef
	}{
		{"", f.own, domain.RunWorktreeRef{Key: "own", Kind: domain.RunWorktreeOwn, Branch: "agent/own", SessionID: "s1"}},
		{"own", f.own, domain.RunWorktreeRef{Key: "own", Kind: domain.RunWorktreeOwn, Branch: "agent/own", SessionID: "s1"}},
		{"main", f.main, domain.RunWorktreeRef{Key: "main", Kind: domain.RunWorktreeMain, Branch: "main"}},
		{"session:s3", f.w3, domain.RunWorktreeRef{Key: "session:s3", Kind: domain.RunWorktreeSession, Branch: "agent/newer", SessionID: "s3"}},
	} {
		r, err := f.svc.StartIn(ctx, "s1", "", "pwd; sleep 30", c.key)
		if err != nil {
			t.Fatalf("%q: %v", c.key, err)
		}
		if r.Dir != c.dir || r.Worktree != c.want || r.SessionID != "s1" {
			t.Errorf("%q: run = %+v, want dir %s worktree %+v", c.key, r, c.dir, c.want)
		}
		readUntil(t, f.svc, r.ID, lastSegment(c.dir))
	}
	runs, _ := f.svc.List(ctx, "s1")
	if len(runs) != 4 {
		t.Fatalf("every run lists under the session that started it: %+v", runs)
	}
	if other, _ := f.svc.List(ctx, "s3"); len(other) != 0 {
		t.Fatalf("a run started elsewhere shows in the owner's list: %+v", other)
	}
}

func TestARunWorktreeThatIsNotOfferedIsRefusedAndNothingStarts(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	if err := os.RemoveAll(f.w2); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session:s2", "session:x1", "session:ghost", "/etc", "elsewhere"} {
		_, err := f.svc.StartIn(ctx, "s1", "", "sleep 30", key)
		if errs.Code(err) != "WORKTREE_NOT_AVAILABLE" || !strings.Contains(err.Error(), "that worktree is no longer available") {
			t.Errorf("%q: got %v", key, err)
		}
	}
	if runs, _ := f.svc.List(ctx, "s1"); len(runs) != 0 {
		t.Fatalf("a refused run is recorded: %+v", runs)
	}
}

func TestASavedCommandRunsOncePerSessionAndRunWorktree(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	own, err := f.svc.StartIn(ctx, "s1", "greet", "", "")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := f.svc.StartIn(ctx, "s1", "greet", "", "own")
	if again.ID != own.ID {
		t.Fatalf("same worktree reopens the run: %s vs %s", again.ID, own.ID)
	}
	inMain, err := f.svc.StartIn(ctx, "s1", "greet", "", "main")
	if err != nil || inMain.ID == own.ID || inMain.Dir != f.main {
		t.Fatalf("another worktree starts a new run: %+v %v", inMain, err)
	}
	mainAgain, _ := f.svc.StartIn(ctx, "s1", "greet", "", "main")
	if mainAgain.ID != inMain.ID {
		t.Fatalf("main reopens its run: %s vs %s", mainAgain.ID, inMain.ID)
	}
}

// envWrites records where env files are written.
type envWrites struct{ roots []string }

func (e *envWrites) Read(string, string) ([]byte, error) { return nil, nil }
func (e *envWrites) Write(root string, _ []*domain.EnvFile) error {
	e.roots = append(e.roots, root)
	return nil
}

func TestARunInTheMainCheckoutWritesNoEnvFilesButExportsTheSavedDotenv(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	io := &envWrites{}
	f.svc.UseEnvFiles(envFiles{"r1": {{RepositoryID: "r1", Path: ".env", Content: "GREETING=saved\n"}}}, io)

	r, err := f.svc.StartIn(ctx, "s1", "", `echo "got:$GREETING"; sleep 30`, "main")
	if err != nil {
		t.Fatal(err)
	}
	screen := readUntil(t, f.svc, r.ID, "got:saved")
	if len(io.roots) != 0 {
		t.Fatalf("env files were written into %v", io.roots)
	}
	if !strings.HasPrefix(screen, "[harness] Main checkout: env files were not written here.") {
		t.Fatalf("the first line must say so: %q", screen)
	}

	// Session worktrees keep getting the files, and no note.
	for key, dir := range map[string]string{"own": f.own, "session:s3": f.w3} {
		io.roots = nil
		r, err := f.svc.StartIn(ctx, "s1", "", `echo "got:$GREETING"; sleep 30 # `+key, key)
		if err != nil {
			t.Fatal(err)
		}
		screen := readUntil(t, f.svc, r.ID, "got:saved")
		if len(io.roots) != 1 || io.roots[0] != dir {
			t.Errorf("%s: env files written into %v, want %s", key, io.roots, dir)
		}
		if strings.Contains(screen, "were not written") {
			t.Errorf("%s: unexpected note %q", key, screen)
		}
	}
}

func TestAMainRunWithNoSavedEnvFilesStillSaysNothingWasWritten(t *testing.T) {
	f := newWorktreeFixture(t)
	f.svc.UseEnvFiles(envFiles{}, &envWrites{})
	r, err := f.svc.StartIn(context.Background(), "s1", "", `echo up; sleep 30`, "main")
	if err != nil {
		t.Fatal(err)
	}
	screen := readUntil(t, f.svc, r.ID, "up")
	if !strings.HasPrefix(screen, "[harness] Main checkout: env files were not written here.") {
		t.Fatalf("screen = %q", screen)
	}
}

// A session whose own worktree is the main checkout never gets env files
// written into it.
func TestASessionRunningInTheMainCheckoutGetsNoEnvFiles(t *testing.T) {
	root := t.TempDir()
	all := sessions{"s1": {ID: "s1", ProjectID: "p1", RepositoryID: "r1", WorkingDir: root}}
	svc, _ := newRunnerWith(t, all)
	io := &envWrites{}
	svc.UseEnvFiles(envFiles{"r1": {{RepositoryID: "r1", Path: ".env", Content: "A=1\n"}}}, io)
	svc.UseRunWorktrees(repos{"r1": {ID: "r1", RootDir: root}}, nil, nil)
	if _, err := svc.StartIn(context.Background(), "s1", "", "sleep 30", ""); err != nil {
		t.Fatal(err)
	}
	if len(io.roots) != 0 {
		t.Fatalf("env files were written into the main checkout: %v", io.roots)
	}
}
