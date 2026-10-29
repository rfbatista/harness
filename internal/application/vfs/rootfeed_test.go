package vfs_test

import (
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type sinkRecorder struct {
	mu  sync.Mutex
	got []ports.ProjectChange
}

func (s *sinkRecorder) AnnounceChange(c ports.ProjectChange) {
	s.mu.Lock()
	s.got = append(s.got, c)
	s.mu.Unlock()
}

func (s *sinkRecorder) wait(t *testing.T, n int) []ports.ProjectChange {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.got) >= n {
			out := append([]ports.ProjectChange(nil), s.got...)
			s.mu.Unlock()
			return out
		}
		s.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("wanted %d changes, got %d", n, len(s.got))
	return nil
}

func TestEndingASessionAnnouncesItsResourcesRootGoneOnceOnTheProjectFeed(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionDone)
	w.sess.ProjectID = "p1"
	sink := &sinkRecorder{}
	stop := w.fs.AnnounceRoots(sink)
	defer stop()
	dir := w.open(t, "s1")
	w.resources.Close("s1", dir)

	got := sink.wait(t, 2)
	time.Sleep(50 * time.Millisecond)
	var gone []ports.SessionFilesRootChange
	for _, c := range got {
		if c.SessionFilesRoot != nil && !c.SessionFilesRoot.Available {
			gone = append(gone, *c.SessionFilesRoot)
		}
	}
	if len(gone) != 1 {
		t.Fatalf("%d root-gone changes in %+v", len(gone), got)
	}
	g := gone[0]
	if g.Type != "session_files_root" || g.SessionID != "s1" || g.Root != "resources" || g.Note != "The resources folder ended with the session." || g.ProjectID != "p1" {
		t.Errorf("change = %+v", g)
	}
	if got[0].SessionFilesRoot == nil || !got[0].SessionFilesRoot.Available || got[0].SessionFilesRoot.Note != "" {
		t.Errorf("opening announces the root available: %+v", got[0])
	}
}

func TestRemovingTheWorktreeAnnouncesItsRootGone(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionDone)
	w.sess.ProjectID = "p1"
	sink := &sinkRecorder{}
	stop := w.fs.AnnounceRoots(sink)
	defer stop()
	w.fs.WorkspaceRemoved(&domain.Workspace{ID: "w1", Path: w.worktree})
	c := sink.wait(t, 1)[0].SessionFilesRoot
	if c == nil || c.Root != "worktree" || c.Available || c.Note != "The worktree was removed." {
		t.Errorf("change = %+v", c)
	}
}
