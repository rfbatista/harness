package vshell_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/ports"
)

// fakeRoots is a resources root opener that records what it was asked.
type fakeRoots struct {
	opened []string
	closed [][2]string
	scope  ports.ShellScope
	err    error
}

func (f *fakeRoots) Open(_ context.Context, id string, scope ports.ShellScope) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.opened = append(f.opened, id)
	f.scope = scope
	return "/tmp/harness-shell-" + id, nil
}

func (f *fakeRoots) Close(id, dir string) { f.closed = append(f.closed, [2]string{id, dir}) }

func TestStartingAnAgentsShellOpensItsResourcesRootAndNamesTheFolder(t *testing.T) {
	roots := &fakeRoots{}
	sh, err := vshell.Start(context.Background(), roots, "s1", ports.ShellScope{TicketID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if sh.Dir != "/tmp/harness-shell-s1" {
		t.Fatalf("Dir = %q", sh.Dir)
	}
	if got := sh.Env(); len(got) != 1 || got[0] != "HARNESS_SHELL=/tmp/harness-shell-s1" {
		t.Fatalf("Env = %v", got)
	}
	if roots.scope.TicketID != "t1" {
		t.Fatalf("opened on %+v", roots.scope)
	}
	sh.Stop()
	if len(roots.closed) != 1 || roots.closed[0] != [2]string{"s1", "/tmp/harness-shell-s1"} {
		t.Fatalf("closed = %v", roots.closed)
	}
}

func TestAnAgentsShellThatCannotOpenSaysSo(t *testing.T) {
	boom := errors.New("boom")
	_, err := vshell.Start(context.Background(), &fakeRoots{err: boom}, "s1", ports.ShellScope{TicketID: "t1"})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "make the session's virtual shell") {
		t.Fatalf("err = %v", err)
	}
}
