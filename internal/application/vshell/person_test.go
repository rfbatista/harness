package vshell_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/personshell"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// folders is a virtual file system of one session "s1": real paths by root
// and folder.
type folders map[string]string

func (f folders) RealPath(_ context.Context, sessionID string, kind vfs.RootKind, folder string) (string, error) {
	if sessionID != "s1" {
		return "", &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no such session"}
	}
	p, ok := f[string(kind)+":"+folder]
	switch {
	case ok:
		return p, nil
	case kind == vfs.ResourcesRoot && folder == "gone":
		return "", &domain.StructuredError{Code: "ROOT_UNAVAILABLE", Message: "gone"}
	case kind != vfs.ResourcesRoot && kind != vfs.WorktreeRoot:
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "unknown root"}
	}
	return "", &domain.StructuredError{Code: "PATH_NOT_FOUND", Message: folder + " is not there"}
}

type sessions map[string]bool

func (s sessions) Get(id string) *domain.Session {
	if s[id] {
		return &domain.Session{ID: id}
	}
	return nil
}

// person is the person's terminals on a real PTY host, with serverEnv as the
// server's environment.
type person struct {
	*vshell.PersonTerminals
	worktree string
}

func newPerson(t *testing.T, serverEnv []string) person {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "web", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	pty := ptyunix.New()
	host := termhost.New(shell.Direct{}, pty).WithLaunchers(personshell.New(pty, func() []string { return vshell.PersonEnv(serverEnv) }))
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	svc := vshell.NewPersonTerminals(vshell.PersonDeps{
		Host:     host,
		Folders:  folders{"worktree:": worktree, "worktree:web/src": filepath.Join(worktree, "web", "src")},
		Sessions: sessions{"s1": true},
	})
	return person{svc, worktree}
}

// run types a line into the terminal and waits for want on its screen.
func run(t *testing.T, term ports.Terminal, line, want string) {
	t.Helper()
	for _, r := range line {
		_ = term.Key(ports.KeyEvent{Code: r, Text: string(r)})
	}
	_ = term.Key(ports.KeyEvent{Code: '\r'})
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, sub := term.Subscribe()
		sub.Close()
		if screen := ansi.Strip(snap.Screen); strings.Contains(screen, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q never showed %q; the screen is\n%s", line, want, ansi.Strip(snap.Screen))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func open(t *testing.T, p person, root, dir string) (ports.PersonTerminal, ports.Terminal) {
	t.Helper()
	pt, err := p.Open(context.Background(), "s1", ports.PersonTerminalOpen{Root: root, Dir: dir, Size: ports.TermSize{Cols: 300, Rows: 30}})
	if err != nil {
		t.Fatal(err)
	}
	term, err := p.Attach(pt.ID)
	if err != nil {
		t.Fatal(err)
	}
	return pt, term
}

func TestOpenARunningShellOnAWorktreeFolder(t *testing.T) {
	p := newPerson(t, os.Environ())
	pt, term := open(t, p, "worktree", "web/src")

	if !strings.HasPrefix(pt.ID, "pt_") || pt.SessionID != "s1" || pt.Root != "worktree" || pt.Dir != "web/src" || pt.Label != "web/src" || pt.State != ports.PersonTerminalRunning || pt.OpenedAt.IsZero() {
		t.Errorf("terminal = %+v", pt)
	}
	// $(pwd -P): the real path, as temp dirs can be symlinks.
	run(t, term, `echo cwd=$(pwd -P)`, "cwd="+realPath(t, filepath.Join(p.worktree, "web", "src")))
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestListIsTheSessionsTerminalsOldestFirstAndCloseEndsOne(t *testing.T) {
	p := newPerson(t, os.Environ())
	a, _ := open(t, p, "worktree", "")
	b, _ := open(t, p, "worktree", "web/src")

	got, err := p.List(context.Background(), "s1")
	if err != nil || len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("list = %+v, %v; want %s then %s", got, err, a.ID, b.ID)
	}
	if got[0].Label != "Worktree" {
		t.Errorf("the root's label = %q, want Worktree", got[0].Label)
	}

	if err := p.Close(a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = p.List(context.Background(), "s1")
	if len(got) != 1 || got[0].ID != b.ID {
		t.Errorf("after closing %s the list is %+v", a.ID, got)
	}
	if _, err := p.Attach(a.ID); errs.Code(err) != "TERMINAL_NOT_FOUND" {
		t.Errorf("attach to a closed terminal = %v, want TERMINAL_NOT_FOUND", err)
	}
	if err := p.Close(a.ID); err != nil {
		t.Errorf("closing a closed terminal = %v, want nil", err)
	}
}

func TestOpenRefusals(t *testing.T) {
	p := newPerson(t, os.Environ())
	for name, c := range map[string]struct {
		session string
		req     ports.PersonTerminalOpen
		code    string
	}{
		"unknown session": {"nope", ports.PersonTerminalOpen{Root: "worktree"}, "SESSION_NOT_FOUND"},
		"bad root":        {"s1", ports.PersonTerminalOpen{Root: "home"}, "INVALID_INPUT"},
		"missing folder":  {"s1", ports.PersonTerminalOpen{Root: "worktree", Dir: "nope"}, "PATH_NOT_FOUND"},
		"root gone":       {"s1", ports.PersonTerminalOpen{Root: "resources", Dir: "gone"}, "ROOT_GONE"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := p.Open(context.Background(), c.session, c.req)
			if errs.Code(err) != c.code {
				t.Errorf("Open = %v, want %s", err, c.code)
			}
		})
	}
	if _, err := p.List(context.Background(), "nope"); errs.Code(err) != "SESSION_NOT_FOUND" {
		t.Errorf("List of an unknown session = %v", err)
	}
}

// The person's shell is theirs, not the agent's: whatever the harness set for
// agents is gone, what the person's tools need stays.
func TestTheShellRunsInTheServersEnvironmentWithoutTheHarnessEntries(t *testing.T) {
	server := append(os.Environ(),
		"HARNESS_SHELL=/x", "HARNESS_SANDBOX=seatbelt", "HARNESS_SHIM_DIR=/tmp/harness-shims-1", "CODING_POOL_TOKEN=secret",
		"CLAUDE_CONFIG_DIR=/agent/home", "SSH_AUTH_SOCK=/tmp/agent.sock", "LANG=C", "HOME="+t.TempDir(),
	)
	p := newPerson(t, server)
	_, term := open(t, p, "worktree", "")

	run(t, term, `echo "env:$(env | grep -c -E '^(HARNESS_|CODING_POOL_|CLAUDE_CONFIG_DIR)')"`, "env:0")
	run(t, term, `echo "sock=$SSH_AUTH_SOCK lang=$LANG"`, "sock=/tmp/agent.sock lang=C")
	// The agent sandbox does not apply, whatever its switch says: the home is
	// the person's, and writable.
	run(t, term, `touch "$HOME/mine" && echo wrote-home`, "wrote-home")
}
