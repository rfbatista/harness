package workspaces_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// scriptedSnapshots answers each worktree's current fingerprint.
type scriptedSnapshots struct {
	ports.GitCode
	mu    sync.Mutex
	state map[string]string // path → fingerprint
}

func (g *scriptedSnapshots) set(path, fp string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state[path] = fp
}

func (g *scriptedSnapshots) Snapshot(_ context.Context, dir string) (string, string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fp, ok := g.state[dir]
	if !ok {
		return "", "", os.ErrNotExist
	}
	return "head-" + fp, fp, nil
}

type changeLog struct {
	mu      sync.Mutex
	changes []ports.CodeChange
}

func (l *changeLog) AnnounceChange(c ports.ProjectChange) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if c.CodeChanged != nil {
		l.changes = append(l.changes, *c.CodeChanged)
	}
}

func (l *changeLog) take() []ports.CodeChange {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.changes
	l.changes = nil
	return out
}

type watchFixture struct {
	watcher  *workspaces.CodeWatcher
	bus      *eventbus.Bus
	git      *scriptedSnapshots
	feed     *changeLog
	sessions ports.SessionRepository
	ws, ws2  *domain.Workspace
	now      time.Time
}

func newWatchFixture(t *testing.T) *watchFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	wsRepo := sqlite.NewWorkspaceRepository(db)
	ws, _ := wsRepo.Create(domain.Workspace{RepositoryID: "r1", Name: "a", Branch: "a", Path: "/wt/a"})
	ws2, _ := wsRepo.Create(domain.Workspace{RepositoryID: "r1", Name: "b", Branch: "b", Path: "/wt/b"})
	f := &watchFixture{
		bus: eventbus.New(), git: &scriptedSnapshots{state: map[string]string{"/wt/a": "1", "/wt/b": "1"}},
		feed: &changeLog{}, sessions: sqlite.NewSessionRepository(db), ws: ws, ws2: ws2,
		now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC),
	}
	f.watcher = workspaces.NewCodeWatcher(workspaces.CodeWatcherDeps{
		Sessions: f.sessions, Workspaces: wsRepo, Git: f.git, Feed: f.feed, Now: func() time.Time { return f.now },
	})
	f.watcher.Subscribe(f.bus)
	return f
}

func (f *watchFixture) session(t *testing.T, id, wsID string, status domain.SessionStatus) {
	t.Helper()
	if _, err := f.sessions.Create(&domain.Session{ID: id, ProjectID: "p1", WorkspaceID: wsID, Status: status}); err != nil {
		t.Fatal(err)
	}
}

func (f *watchFixture) publish(t *testing.T, ev domain.Event) {
	t.Helper()
	if err := f.bus.Publish(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
}

// poll looks once, the interval after the last look.
func (f *watchFixture) poll() []ports.CodeChange {
	f.now = f.now.Add(workspaces.CodeWatchInterval)
	f.watcher.Poll(context.Background())
	return f.feed.take()
}

func TestCodeWatcherAnnouncesAChangeOncePerChange(t *testing.T) {
	f := newWatchFixture(t)
	f.session(t, "s1", f.ws.ID, domain.SessionStarting)
	f.publish(t, domain.SessionStarted{SessionID: "s1", ProjectID: "p1"})
	if got := f.poll(); len(got) != 0 {
		t.Fatalf("the first look announced %v", got)
	}
	f.git.set("/wt/a", "2")
	got := f.poll()
	want := []ports.CodeChange{{WorkspaceID: f.ws.ID, Head: "head-2", ProjectID: "p1"}}
	if !slices.Equal(got, want) {
		t.Fatalf("changes = %+v, want %+v", got, want)
	}
	if got := f.poll(); len(got) != 0 {
		t.Fatalf("an unchanged worktree announced %v", got)
	}
}

func TestCodeWatcherAnnouncesAtMostOncePerInterval(t *testing.T) {
	f := newWatchFixture(t)
	f.session(t, "s1", f.ws.ID, domain.SessionRunning)
	f.watcher.Track("s1")
	f.poll()
	f.git.set("/wt/a", "2")
	if got := f.poll(); len(got) != 1 {
		t.Fatalf("changes = %v", got)
	}
	// A second change half an interval later waits for the next one.
	f.git.set("/wt/a", "3")
	f.now = f.now.Add(workspaces.CodeWatchInterval / 2)
	f.watcher.Poll(context.Background())
	if got := f.feed.take(); len(got) != 0 {
		t.Fatalf("announced within the interval: %v", got)
	}
	if got := f.poll(); len(got) != 1 || got[0].Head != "head-3" {
		t.Fatalf("the held change = %v", got)
	}
}

func TestCodeWatcherWatchesOnlyWhileASessionRuns(t *testing.T) {
	f := newWatchFixture(t)
	f.session(t, "s1", f.ws.ID, domain.SessionRunning)
	f.session(t, "s2", f.ws.ID, domain.SessionRunning)
	f.session(t, "s3", "", domain.SessionRunning)
	f.publish(t, domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionRunning})
	f.publish(t, domain.SessionStatusChanged{SessionID: "s2", Status: domain.SessionThinking})
	f.publish(t, domain.SessionStatusChanged{SessionID: "s3", Status: domain.SessionRunning})
	if w := f.watcher.Watched(); !slices.Equal(w, []string{f.ws.ID}) {
		t.Fatalf("watched = %v", w)
	}
	f.publish(t, domain.SessionEnded{SessionID: "s1", Status: domain.SessionDone})
	if w := f.watcher.Watched(); len(w) != 1 {
		t.Fatalf("one of two sessions ended: watched = %v", w)
	}
	f.publish(t, domain.SessionStatusChanged{SessionID: "s2", Status: domain.SessionStopped})
	if w := f.watcher.Watched(); len(w) != 0 {
		t.Fatalf("every session ended: watched = %v", w)
	}
	f.poll()
	f.git.set("/wt/a", "2")
	if got := f.poll(); len(got) != 0 {
		t.Fatalf("announced for a workspace with no running session: %v", got)
	}
	// Resumed: watched again.
	f.publish(t, domain.SessionStatusChanged{SessionID: "s2", Status: domain.SessionRunning})
	if w := f.watcher.Watched(); len(w) != 1 {
		t.Fatalf("a resumed session: watched = %v", w)
	}
	f.publish(t, domain.SessionDeleted{SessionID: "s2"})
	if w := f.watcher.Watched(); len(w) != 0 {
		t.Fatalf("a deleted session: watched = %v", w)
	}
}

func TestCodeWatcherStartsWithTheSessionsAlreadyRunning(t *testing.T) {
	f := newWatchFixture(t)
	f.session(t, "s1", f.ws.ID, domain.SessionIdle)
	f.session(t, "s2", f.ws2.ID, domain.SessionDone)
	f.watcher.Start()
	defer f.watcher.Stop()
	if w := f.watcher.Watched(); !slices.Equal(w, []string{f.ws.ID}) {
		t.Fatalf("watched = %v", w)
	}
}

func TestCodeWatcherSkipsAWorktreeGoneFromDisk(t *testing.T) {
	f := newWatchFixture(t)
	f.session(t, "s1", f.ws.ID, domain.SessionRunning)
	f.watcher.Track("s1")
	f.git.mu.Lock()
	delete(f.git.state, "/wt/a")
	f.git.mu.Unlock()
	if got := f.poll(); len(got) != 0 {
		t.Fatalf("changes = %v", got)
	}
}

func TestGitSnapshotSeesAnAlreadyModifiedFileEditedAgain(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(content string, at time.Time) {
		t.Helper()
		p := filepath.Join(dir, "a.txt")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write("one\n", time.Now().Add(-time.Hour))
	run("add", "-A")
	run("commit", "-q", "-m", "one")
	git := gitcli.NewCode()
	ctx := context.Background()
	_, clean, err := git.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write("two\n", time.Now().Add(-time.Minute))
	_, modified, _ := git.Snapshot(ctx, dir)
	write("three\n", time.Now())
	_, again, _ := git.Snapshot(ctx, dir)
	if clean == modified || modified == again {
		t.Fatalf("fingerprints: clean %s, modified %s, edited again %s", clean, modified, again)
	}
	if _, _, err := git.Snapshot(ctx, filepath.Join(dir, "gone")); err == nil {
		t.Error("a missing worktree has a snapshot")
	}
}
