package vfs_test

import (
	"testing"
	"time"

	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// listen collects what the world's root events say.
func (w *world) listen(t *testing.T) <-chan vfs.RootEvent {
	t.Helper()
	ch := make(chan vfs.RootEvent, 16)
	cancel := w.events.Subscribe(func(ev vfs.RootEvent) { ch <- ev })
	t.Cleanup(cancel)
	return ch
}

func next(t *testing.T, ch <-chan vfs.RootEvent) vfs.RootEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no root event")
		return vfs.RootEvent{}
	}
}

func none(t *testing.T, ch <-chan vfs.RootEvent) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected root event %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestOpeningAResourcesRootRaisesAvailableAndClosingItRaisesGoneOnce(t *testing.T) {
	w := newWorld(t)
	ev := w.listen(t)
	dir := w.open(t, "s1")
	got := next(t, ev)
	if got.SessionID != "s1" || got.Kind != vfs.ResourcesRoot || !got.Available || got.Path == "" {
		t.Fatalf("on open: %+v", got)
	}
	path := got.Path
	w.resources.Close("s1", dir)
	got = next(t, ev)
	if got.SessionID != "s1" || got.Kind != vfs.ResourcesRoot || got.Available || got.Path != path {
		t.Fatalf("on close: %+v", got)
	}
	w.resources.Close("s1", dir)
	none(t, ev)
}

func TestClosingAnOldResourcesRootRaisesNothing(t *testing.T) {
	w := newWorld(t)
	old := w.open(t, "s1")
	ev := w.listen(t)
	w.resources.Close("s1", old+"-not-its-root")
	none(t, ev)
}

func TestOpeningAResourcesRootAgainMovesTheFolderAndRaisesGoneThenAvailable(t *testing.T) {
	w := newWorld(t)
	w.open(t, "s1")
	oldPath, _ := w.resources.RootDir("s1")
	ev := w.listen(t)
	w.open(t, "s1")
	newPath, _ := w.resources.RootDir("s1")
	if newPath == oldPath {
		t.Fatal("the replacement kept the path; the test's premise is wrong")
	}
	gone, avail := next(t, ev), next(t, ev)
	if gone.Available || gone.Path != oldPath || !avail.Available || avail.Path != newPath {
		t.Fatalf("gone = %+v, available = %+v", gone, avail)
	}
	none(t, ev)
}

func TestRemovingAWorkspaceRaisesWorktreeGoneOnceForEverySessionOfIt(t *testing.T) {
	w := newWorld(t)
	ev := w.listen(t)
	w.fs.WorkspaceRemoved(&domain.Workspace{ID: "w1", Path: w.worktree})
	seen := map[string]bool{}
	for range 2 {
		got := next(t, ev)
		if got.Kind != vfs.WorktreeRoot || got.Available || got.Path != w.worktree || seen[got.SessionID] {
			t.Fatalf("event %+v", got)
		}
		seen[got.SessionID] = true
	}
	if !seen["s1"] || !seen["s2"] {
		t.Fatalf("heard for %v", seen)
	}
	none(t, ev)
}

func TestASlowSubscriberDoesNotBlockTheRemoverAndALateOneHearsLaterEvents(t *testing.T) {
	w := newWorld(t)
	block := make(chan struct{})
	cancelSlow := w.events.Subscribe(func(vfs.RootEvent) { <-block })
	defer func() { close(block); cancelSlow() }()
	done := make(chan struct{})
	go func() {
		for range 50 {
			w.fs.WorkspaceRemoved(&domain.Workspace{ID: "w1", Path: w.worktree})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a slow subscriber blocked the remover")
	}
	late := w.listen(t)
	w.fs.WorkspaceRemoved(&domain.Workspace{ID: "w2", Path: "/x"})
	if got := next(t, late); got.SessionID != "other" {
		t.Fatalf("late subscriber heard %+v", got)
	}
}

var _ ports.WorkspaceRemovalSink = (*vfs.FileSystem)(nil)
