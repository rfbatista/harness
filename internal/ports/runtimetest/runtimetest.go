// Package runtimetest holds contract suites for the runtime stack's driven
// ports — Shell and PTY — and Script, an Agent that runs a shell script in
// place of an agent CLI so hosts and panes can be tested without claude.
package runtimetest

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/ports"
)

// ScriptKind is the AgentSpec.Kind Script serves.
const ScriptKind = "script"

// Script is a ports.Agent that runs spec.Prompt as a /bin/sh script in
// spec.Dir, with spec.Env.
type Script struct{}

func (Script) Kind() string { return ScriptKind }

func (Script) Command(spec ports.AgentSpec) (ports.ShellCommand, error) {
	return ports.ShellCommand{Program: "/bin/sh", Args: []string{"-c", spec.Prompt}, Dir: spec.Dir, Env: spec.Env}, nil
}

// Spec is an AgentSpec that runs script in dir on the Script agent.
func Spec(id, dir, script string) ports.AgentSpec {
	return ports.AgentSpec{Kind: ScriptKind, SessionID: id, Dir: dir, Prompt: script, Conversation: ports.Conversation{ID: id}}
}

// ShellConformance runs the Shell contract: the prepared process runs the
// program with its arguments, in its directory, with its environment added,
// and CheckDir tells a missing directory apart.
func ShellConformance(t *testing.T, newShell func(t *testing.T) ports.Shell) {
	ctx := context.Background()

	t.Run("prepared process runs the command", func(t *testing.T) {
		sh := newShell(t)
		dir := t.TempDir()
		spec, err := sh.Prepare(ctx, ports.ShellCommand{
			Program: "sh",
			Args:    []string{"-c", `printf '%s|%s|%s' "$1" "$RUNTIMETEST_VAR" "$(pwd -P)"`, "sh", "an arg"},
			Dir:     dir,
			Env:     []string{"RUNTIMETEST_VAR=set"},
		})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(spec.Path, spec.Args...)
		cmd.Dir, cmd.Env = spec.Dir, spec.Env
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("run %q %q: %v", spec.Path, spec.Args, err)
		}
		want := "an arg|set|" + realpath(t, dir)
		if !strings.HasSuffix(string(out), want) {
			t.Fatalf("output = %q, want it to end with %q", out, want)
		}
	})

	t.Run("CheckDir", func(t *testing.T) {
		sh := newShell(t)
		if err := sh.CheckDir(ctx, t.TempDir()); err != nil {
			t.Fatalf("existing dir: %v", err)
		}
		if got := errs.Code(sh.CheckDir(ctx, t.TempDir()+"/missing")); got != "WORKSPACE_MISSING" {
			t.Fatalf("missing dir code = %q, want WORKSPACE_MISSING", got)
		}
	})
}

func realpath(t *testing.T, dir string) string {
	out, err := exec.Command("sh", "-c", "cd '"+dir+"' && pwd -P").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// PTYConformance runs the PTY contract: output reads back, input is typed,
// the size and TERM are the terminal's, resizing reaches the process, and
// Wait reports the exit code.
func PTYConformance(t *testing.T, pty ports.PTY) {
	start := func(t *testing.T, script string, size ports.TermSize) ports.PTYProcess {
		t.Helper()
		p, err := pty.Start(ports.ProcessSpec{Path: "/bin/sh", Args: []string{"-c", script}, Dir: t.TempDir()}, size)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}

	t.Run("output, input and exit code", func(t *testing.T) {
		p := start(t, `printf 'ready\n'; read x; printf 'got:%s\n' "$x"; exit 3`, ports.TermSize{Cols: 80, Rows: 24})
		r := newReader(p)
		r.until(t, "ready")
		if _, err := p.Write([]byte("hello\r")); err != nil {
			t.Fatal(err)
		}
		r.until(t, "got:hello")
		code, err := p.Wait()
		if err != nil || code != 3 {
			t.Fatalf("Wait = %d, %v; want 3", code, err)
		}
	})

	t.Run("size, TERM and resize", func(t *testing.T) {
		p := start(t, `printf 'size:%s term:%s\n' "$(stty size)" "$TERM"; read x; printf 'size:%s\n' "$(stty size)"`, ports.TermSize{Cols: 91, Rows: 17})
		r := newReader(p)
		r.until(t, "size:17 91 term:xterm-256color")
		if err := p.Resize(ports.TermSize{Cols: 100, Rows: 30}); err != nil {
			t.Fatal(err)
		}
		_, _ = p.Write([]byte("\r"))
		r.until(t, "size:30 100")
	})
}

// reader collects a process's output in the background.
type reader struct {
	mu  chan struct{}
	buf bytes.Buffer
}

func newReader(src io.Reader) *reader {
	r := &reader{mu: make(chan struct{}, 1)}
	r.mu <- struct{}{}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := src.Read(b)
			<-r.mu
			r.buf.Write(b[:n])
			r.mu <- struct{}{}
			if err != nil {
				return
			}
		}
	}()
	return r
}

func (r *reader) until(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		<-r.mu
		got := r.buf.String()
		r.mu <- struct{}{}
		if strings.Contains(got, want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-r.mu
	defer func() { r.mu <- struct{}{} }()
	t.Fatalf("timed out waiting for %q; output so far: %q", want, r.buf.String())
}

// TerminalConformance runs the Terminal contract, local or attached over the
// wire alike. open runs script on a fresh 80x24 terminal and returns it.
func TerminalConformance(t *testing.T, open func(t *testing.T, script string) ports.Terminal) {
	t.Run("output follows the snapshot, keys are typed", func(t *testing.T) {
		term := open(t, `printf 'before\r\n'; read x; printf 'after:%s\r\n' "$x"; sleep 5`)
		w := watch(term)
		w.until(t, "before")
		for _, r := range "hi" {
			_ = term.Key(ports.KeyEvent{Code: r, Text: string(r)})
		}
		_ = term.Key(ports.KeyEvent{Code: '\r'})
		w.until(t, "after:hi")
	})

	t.Run("resize reaches the program", func(t *testing.T) {
		term := open(t, `read x; printf 'size:%s\r\n' "$(stty size)"; sleep 5`)
		w := watch(term)
		if err := term.Resize(ports.TermSize{Cols: 100, Rows: 30}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond) // the resize travels on its own
		_ = term.Key(ports.KeyEvent{Code: '\r'})
		w.until(t, "size:30 100")
	})

	t.Run("title", func(t *testing.T) {
		term := open(t, `printf '\033]0;fix flaky tests\007'; sleep 5`)
		deadline := time.Now().Add(5 * time.Second)
		for term.Title() != "fix flaky tests" {
			if time.Now().After(deadline) {
				t.Fatalf("Title = %q, want the one the program set", term.Title())
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	t.Run("exit code", func(t *testing.T) {
		term := open(t, `printf 'bye\r\n'; exit 3`)
		select {
		case <-term.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("Done never closed")
		}
		if code := term.ExitCode(); code != 3 {
			t.Fatalf("ExitCode = %d, want 3", code)
		}
	})

	t.Run("kill", func(t *testing.T) {
		term := open(t, `sleep 30`)
		if err := term.Kill(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-term.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("Kill returned with the process still running")
		}
	})
}

// watcher collects a terminal's screen text: the snapshot, then the output.
type watcher struct {
	mu  chan struct{}
	buf strings.Builder
}

func watch(term ports.Terminal) *watcher {
	w := &watcher{mu: make(chan struct{}, 1)}
	w.mu <- struct{}{}
	snap, sub := term.Subscribe()
	w.buf.WriteString(snap.Screen)
	go func() {
		for b := range sub.C {
			<-w.mu
			w.buf.Write(b)
			w.mu <- struct{}{}
		}
	}()
	return w
}

func (w *watcher) until(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		<-w.mu
		got := ansi.Strip(w.buf.String())
		w.mu <- struct{}{}
		if strings.Contains(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q; seen: %q", want, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
