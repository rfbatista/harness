package vshell_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	events   *fakeRootEvents
	ended    chan ports.SessionEvent
}

// fakeRootEvents is the virtual file system's root signal, raised by hand.
type fakeRootEvents struct {
	mu  sync.Mutex
	fns []func(vfs.RootEvent)
}

func (f *fakeRootEvents) Subscribe(fn func(vfs.RootEvent)) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fns = append(f.fns, fn)
	return func() {}
}

func (f *fakeRootEvents) raise(ev vfs.RootEvent) {
	f.mu.Lock()
	fns := slices.Clone(f.fns)
	f.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// pageEvents is what the session's followers are sent.
type pageEvents chan ports.SessionEvent

func (p pageEvents) PublishEphemeral(_ string, ev ports.SessionEvent) { p <- ev }

// denyDriver stands in for a platform's sandbox: it runs the command as it is
// and notes the path it was asked to make read-only.
type denyDriver struct{ path chan string }

func (d denyDriver) WrapDenyWrites(path string, cmd ports.SandboxCommand) (ports.SandboxCommand, error) {
	d.path <- path
	return cmd, nil
}

func newPerson(t *testing.T, serverEnv []string) person { return newPersonOn(t, serverEnv, nil) }

func newPersonOn(t *testing.T, serverEnv []string, deny ports.DenyWritesDriver) person {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	worktree := t.TempDir()
	for _, d := range []string{"web/src", "res/docs"} {
		if err := os.MkdirAll(filepath.Join(worktree, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pty := ptyunix.New()
	host := termhost.New(shell.Direct{}, pty).WithLaunchers(personshell.New(pty, func() []string { return vshell.PersonEnv(serverEnv) }, deny))
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	events, ended := &fakeRootEvents{}, make(pageEvents, 8)
	svc := vshell.NewPersonTerminals(vshell.PersonDeps{
		Host:       host,
		RootEvents: events,
		Page:       ended,
		Folders: folders{
			"worktree:": worktree, "worktree:web/src": filepath.Join(worktree, "web", "src"),
			"resources:": filepath.Join(worktree, "res"), "resources:docs": filepath.Join(worktree, "res", "docs"),
		},
		Sessions: sessions{"s1": true},
	})
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	return person{svc, worktree, events, ended}
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

// The resources root is opened under the deny-writes profile of the whole
// root, wherever in it the folder is; the worktree is opened without one.
func TestAResourcesTerminalRunsUnderTheDenyWritesProfileOfItsRoot(t *testing.T) {
	driver := denyDriver{path: make(chan string, 4)}
	p := newPersonOn(t, os.Environ(), driver)

	_, term := open(t, p, "resources", "docs")
	if got, want := <-driver.path, filepath.Join(p.worktree, "res"); got != want {
		t.Errorf("the profile denies writes under %q, want the root %q", got, want)
	}
	run(t, term, `echo cwd=$(pwd -P)`, "cwd="+realPath(t, filepath.Join(p.worktree, "res", "docs")))

	open(t, p, "worktree", "")
	select {
	case got := <-driver.path:
		t.Errorf("a worktree terminal ran under a profile on %q", got)
	default:
	}
}

func TestWithoutASandboxAResourcesTerminalIsRefusedAndAWorktreeOneOpens(t *testing.T) {
	p := newPerson(t, os.Environ())
	_, err := p.Open(context.Background(), "s1", ports.PersonTerminalOpen{Root: "resources", Dir: ""})
	if errs.Code(err) != "TERMINAL_READ_ONLY_UNAVAILABLE" {
		t.Errorf("Open = %v, want TERMINAL_READ_ONLY_UNAVAILABLE", err)
	}
	if got, _ := p.List(context.Background(), "s1"); len(got) != 0 {
		t.Errorf("the refused terminal is listed: %+v", got)
	}
	open(t, p, "worktree", "")
}

// next is the event the page was sent, or a failure when none comes.
func next(t *testing.T, p person) ports.SessionEvent {
	t.Helper()
	select {
	case ev := <-p.ended:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event was sent to the page")
		return ports.SessionEvent{}
	}
}

func noEvent(t *testing.T, p person) {
	t.Helper()
	select {
	case ev := <-p.ended:
		t.Errorf("an event was sent to the page: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

func waitDone(t *testing.T, term ports.Terminal) {
	t.Helper()
	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the terminal did not end")
	}
}

func TestTheSessionEndingClosesItsResourcesTerminalsAndKeepsTheWorktreeOnes(t *testing.T) {
	p := newPersonOn(t, os.Environ(), denyDriver{path: make(chan string, 4)})
	res, resTerm := open(t, p, "resources", "")
	wt, wtTerm := open(t, p, "worktree", "")

	p.events.raise(vfs.RootEvent{SessionID: "s1", Kind: vfs.ResourcesRoot, Path: filepath.Join(p.worktree, "res")})

	ev := next(t, p)
	if ev.Type != "person_terminal_ended" || ev.ID != res.ID || ev.SessionID != "s1" || ev.Reason != "root_gone" || ev.Message != "The session ended, and its resources folder with it." {
		t.Errorf("event = %+v", ev)
	}
	waitDone(t, resTerm)
	if got, _ := p.List(context.Background(), "s1"); len(got) != 1 || got[0].ID != wt.ID {
		t.Errorf("list = %+v, want only %s", got, wt.ID)
	}
	select {
	case <-wtTerm.Done():
		t.Error("the worktree terminal ended with the resources root")
	default:
	}
	noEvent(t, p)
}

func TestRemovingTheWorktreeClosesItsTerminals(t *testing.T) {
	p := newPerson(t, os.Environ())
	wt, term := open(t, p, "worktree", "web/src")

	p.events.raise(vfs.RootEvent{SessionID: "s1", Kind: vfs.WorktreeRoot, Path: p.worktree})

	ev := next(t, p)
	if ev.ID != wt.ID || ev.Reason != "root_gone" || ev.Message != "The worktree was removed, and its terminals with it." {
		t.Errorf("event = %+v", ev)
	}
	waitDone(t, term)
}

// A root that came back, another session's, or an older root's going away
// does not end a terminal.
func TestOtherRootSignalsLeaveTheTerminalsAlone(t *testing.T) {
	p := newPerson(t, os.Environ())
	_, term := open(t, p, "worktree", "")

	p.events.raise(vfs.RootEvent{SessionID: "s1", Kind: vfs.WorktreeRoot, Path: p.worktree, Available: true})
	p.events.raise(vfs.RootEvent{SessionID: "s2", Kind: vfs.WorktreeRoot, Path: p.worktree})
	p.events.raise(vfs.RootEvent{SessionID: "s1", Kind: vfs.ResourcesRoot, Path: p.worktree})
	p.events.raise(vfs.RootEvent{SessionID: "s1", Kind: vfs.WorktreeRoot, Path: "/an/older/worktree"})

	noEvent(t, p)
	select {
	case <-term.Done():
		t.Error("the terminal ended")
	default:
	}
}

func TestAShellThatExitsEndsItsTerminalWithExited(t *testing.T) {
	p := newPerson(t, os.Environ())
	pt, term := open(t, p, "worktree", "")

	for _, r := range "exit 0" {
		_ = term.Key(ports.KeyEvent{Code: r, Text: string(r)})
	}
	_ = term.Key(ports.KeyEvent{Code: '\r'})

	ev := next(t, p)
	if ev.ID != pt.ID || ev.Reason != "exited" || ev.Message != "The shell exited." {
		t.Errorf("event = %+v", ev)
	}
	if got, _ := p.List(context.Background(), "s1"); len(got) != 0 {
		t.Errorf("an ended terminal is listed: %+v", got)
	}
}

func TestClosingATerminalSendsNoEvent(t *testing.T) {
	p := newPerson(t, os.Environ())
	pt, term := open(t, p, "worktree", "")

	if err := p.Close(pt.ID); err != nil {
		t.Fatal(err)
	}
	waitDone(t, term)
	noEvent(t, p)
}

func TestStoppingTheServerEndsEveryTerminalWithServerStopped(t *testing.T) {
	p := newPerson(t, os.Environ())
	a, _ := open(t, p, "worktree", "")
	b, _ := open(t, p, "worktree", "web/src")

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for range 2 {
		ev := next(t, p)
		got[ev.ID] = ev.Reason
	}
	if got[a.ID] != "server_stopped" || got[b.ID] != "server_stopped" {
		t.Errorf("reasons = %v, want server_stopped for both", got)
	}
}
