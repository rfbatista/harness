package workspaces

import (
	"context"
	"sync"
	"time"

	"operators-mcp/internal/ports"
)

// treeSnapshot is what a workspace's tree reads answer from: the worktree's
// listing and the change state against its base, as of at.
type treeSnapshot struct {
	at      time.Time
	list    []ports.GitEntry
	changes changeSet
}

// treeSnapshots keeps one tree snapshot per workspace. Reads of one
// workspace wait for the refresh in progress instead of listing again, so
// however many directories are read at once, the worktree is listed once
// per watch interval.
type treeSnapshots struct {
	mu    sync.Mutex
	slots map[string]*treeSlot
}

type treeSlot struct {
	mu   sync.Mutex
	snap *treeSnapshot
}

func (t *treeSnapshots) slot(workspaceID string) *treeSlot {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.slots == nil {
		t.slots = map[string]*treeSlot{}
	}
	if t.slots[workspaceID] == nil {
		t.slots[workspaceID] = &treeSlot{}
	}
	return t.slots[workspaceID]
}

func (c *Code) now() time.Time {
	if c.d.Now != nil {
		return c.d.Now()
	}
	return time.Now()
}

// treeSnapshot is the workspace's snapshot, read again from git when it is
// older than the watch interval or force is set.
func (c *Code) treeSnapshot(ctx context.Context, s *source, force bool) (*treeSnapshot, error) {
	slot := c.snaps.slot(s.ws.ID)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if !force && slot.snap != nil && c.now().Sub(slot.snap.at) < CodeWatchInterval {
		return slot.snap, nil
	}
	at := c.now()
	list, err := c.d.Git.WorktreeEntries(ctx, s.ws.Path, false)
	if err != nil {
		return nil, err
	}
	changes, err := c.worktreeChanges(ctx, s, list)
	if err != nil {
		return nil, err
	}
	slot.snap = &treeSnapshot{at: at, list: list, changes: changes}
	return slot.snap, nil
}

// RefreshTree reads the workspace's tree snapshot again now, whatever its
// age. The code watcher calls it when it sees a change, before it announces
// code_changed, so a client that re-reads on the event never gets the old
// listing. A workspace gone from disk has nothing to refresh.
func (c *Code) RefreshTree(ctx context.Context, workspaceID string) error {
	s, err := c.resolveWorkspace(ctx, workspaceID, true)
	if err != nil {
		return err
	}
	_, err = c.treeSnapshot(ctx, s, true)
	return err
}
