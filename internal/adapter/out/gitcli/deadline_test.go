package gitcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// hangingGit puts a fake git first on PATH. Push and fetch answer after
// slowNetwork and succeed; show-ref says the branch does not exist; every
// other command never exits. The hang runs as a child of the script, so it
// keeps git's output pipes open after the script itself is killed, as a git
// whose helper (ssh, a credential helper, a hook) hangs would.
func hangingGit(t *testing.T, slowNetwork time.Duration) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
push|fetch) sleep %.3f; exit 0 ;;
show-ref) exit 1 ;;
esac
sleep 30
`, slowNetwork.Seconds())
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The deadlines the tests shorten stay well above how long macOS takes to
// start a script it has not run before, so only a hang reaches them.

// returnsWithin runs op and fails the test unless it returns within limit.
func returnsWithin(t *testing.T, limit time.Duration, op func() error) (time.Duration, error) {
	t.Helper()
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- op() }()
	select {
	case err := <-done:
		return time.Since(start), err
	case <-time.After(limit):
		t.Fatalf("the git call did not return within %s", limit)
		return 0, nil
	}
}

func TestAWorktreeOperationAgainstAHungGitFailsAfterItsDeadline(t *testing.T) {
	hangingGit(t, 0)
	repo, path := t.TempDir(), filepath.Join(t.TempDir(), "wt")
	// show-ref answers at once; only the worktree runs hang.
	w := NewWorktreeManager(WithReadDeadline(5*time.Second), WithWorktreeDeadline(1500*time.Millisecond))

	took, err := returnsWithin(t, 10*time.Second, func() error { return w.Add(repo, path, "feature", "") })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Add = %v, want a deadline error", err)
	}
	if took < 1500*time.Millisecond {
		t.Errorf("Add gave up after %s, before its worktree deadline: %v", took, err)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = returnsWithin(t, 10*time.Second, func() error { return w.Remove(repo, path) })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Remove = %v, want a deadline error", err)
	}
}

func TestAReadAgainstAHungGitFailsAfterTheReadDeadline(t *testing.T) {
	hangingGit(t, 0)
	dir := t.TempDir()
	read := WithReadDeadline(500 * time.Millisecond)

	took, err := returnsWithin(t, 10*time.Second, func() error {
		_, err := NewCode(read).ResolveCommit(context.Background(), dir, "main")
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ResolveCommit = %v, want a deadline error, not a missing ref", err)
	}
	if took < 500*time.Millisecond {
		t.Errorf("ResolveCommit gave up after %s, before its read deadline", took)
	}

	_, err = returnsWithin(t, 10*time.Second, func() error {
		_, err := NewHistory(read).Status(context.Background(), dir)
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Status = %v, want a deadline error", err)
	}

	_, err = returnsWithin(t, 10*time.Second, func() error {
		_, err := NewWorktreeManager(read).ListBranches(dir)
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ListBranches = %v, want a deadline error", err)
	}
}

func TestPushAndFetchAreNotCutByTheShorterDeadlines(t *testing.T) {
	hangingGit(t, 2*time.Second)
	p := NewBranchPublisher(WithReadDeadline(500*time.Millisecond), WithWorktreeDeadline(500*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := p.Push(ctx, t.TempDir(), "feature", nil); err != nil {
		t.Errorf("Push = %v, want it to finish under its own deadline", err)
	}
	if err := p.FetchBranch(ctx, t.TempDir(), "main", nil); err != nil {
		t.Errorf("FetchBranch = %v, want it to finish under its own deadline", err)
	}
}
