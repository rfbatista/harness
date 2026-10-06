//go:build !windows

package termhost

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/runtimetest"
)

func newHost(t *testing.T) *Host {
	t.Helper()
	h := New(shell.Direct{}, ptyunix.New(), runtimetest.Script{})
	h.killAfter = 200 * time.Millisecond
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	return h
}

// spawn runs script under id and returns its terminal and a channel with the
// exit code onExit reported.
func spawn(t *testing.T, h *Host, id, script string) (ports.Terminal, <-chan int) {
	t.Helper()
	exited := make(chan int, 1)
	if err := h.Spawn(context.Background(), id, runtimetest.Spec(id, t.TempDir(), script), ports.TermSize{Cols: 80, Rows: 24}, func(code int) { exited <- code }); err != nil {
		t.Fatal(err)
	}
	term, err := h.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	return term, exited
}

// screen waits for the terminal's screen to contain want.
func screen(t *testing.T, term ports.Terminal, want string) ports.TerminalSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, sub := term.Subscribe()
		sub.Close()
		if strings.Contains(ansi.Strip(snap.Screen), want) {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q on screen:\n%s", want, ansi.Strip(snap.Screen))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOutputKeysAndExit(t *testing.T) {
	h := newHost(t)
	term, exited := spawn(t, h, "s1", `printf 'hello\r\n'; read x; printf 'typed:%s\r\n' "$x"; exit 4`)
	screen(t, term, "hello")

	for _, r := range "abc" {
		_ = term.Key(ports.KeyEvent{Code: r, Text: string(r)})
	}
	_ = term.Key(ports.KeyEvent{Code: '\r'})
	screen(t, term, "typed:abc")

	select {
	case code := <-exited:
		if code != 4 {
			t.Fatalf("onExit code = %d, want 4", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onExit not called")
	}
	<-term.Done()
	if term.ExitCode() != 4 {
		t.Fatalf("ExitCode = %d, want 4", term.ExitCode())
	}
	if _, err := h.Attach("s1"); errs.Code(err) != "TERMINAL_NOT_FOUND" {
		t.Fatalf("Attach after exit = %v, want TERMINAL_NOT_FOUND", err)
	}
}

// The host answers terminal queries with no client attached; a program that
// asks for the cursor position blocks until it gets an answer.
func TestAnswersTerminalQueriesUnattended(t *testing.T) {
	h := newHost(t)
	term, _ := spawn(t, h, "q", `stty raw -echo; printf '\033[6n'; dd bs=1 count=6 2>/dev/null >/dev/null; stty sane; printf 'answered\r\n'; sleep 5`)
	screen(t, term, "answered")
}

func TestSubscriptionFollowsTheSnapshot(t *testing.T) {
	h := newHost(t)
	term, _ := spawn(t, h, "sub", `printf 'before\r\n'; read x; printf 'after\r\n'; sleep 5`)
	snap := screen(t, term, "before")
	if snap.Size != (ports.TermSize{Cols: 80, Rows: 24}) {
		t.Errorf("snapshot size = %+v", snap.Size)
	}

	_, sub := term.Subscribe()
	defer sub.Close()
	_ = term.Key(ports.KeyEvent{Code: '\r'})
	var got strings.Builder
	deadline := time.After(5 * time.Second)
	for !strings.Contains(got.String(), "after") {
		select {
		case b := <-sub.C:
			got.Write(b)
		case <-deadline:
			t.Fatalf("subscription never carried the output after the snapshot: %q", got.String())
		}
	}
	if strings.Contains(got.String(), "before") {
		t.Errorf("subscription repeated output from before the snapshot: %q", got.String())
	}
}

func TestResizeReachesTheProcess(t *testing.T) {
	h := newHost(t)
	term, _ := spawn(t, h, "rs", `read x; printf 'size:%s\r\n' "$(stty size)"; sleep 5`)
	if err := term.Resize(ports.TermSize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	_ = term.Key(ports.KeyEvent{Code: '\r'})
	snap := screen(t, term, "size:30 100")
	if snap.Size != (ports.TermSize{Cols: 100, Rows: 30}) {
		t.Errorf("snapshot size = %+v, want 100x30", snap.Size)
	}
}

func TestSlowSubscriberIsDroppedNotWaitedFor(t *testing.T) {
	h := newHost(t)
	h.subBuffer = 2
	term, _ := spawn(t, h, "flood", `i=0; while [ $i -lt 20 ]; do printf 'line %d\r\n' $i; sleep 0.02; i=$((i+1)); done; printf 'flood done\r\n'; sleep 5`)
	_, slow := term.Subscribe() // never read
	defer slow.Close()
	screen(t, term, "flood done")

	// The channel closed while the terminal lives on: resubscribe.
	closed := false
	for !closed {
		select {
		case _, ok := <-slow.C:
			closed = !ok
		case <-time.After(5 * time.Second):
			t.Fatal("a subscriber that never reads was not dropped")
		}
	}
	select {
	case <-term.Done():
		t.Fatal("terminal exited; the subscriber was dropped for the wrong reason")
	default:
	}
}

func TestKillAndShutdown(t *testing.T) {
	h := newHost(t)
	a, exitA := spawn(t, h, "a", `trap '' TERM; sleep 30`) // ignores SIGTERM: needs SIGKILL
	b, exitB := spawn(t, h, "b", `sleep 30`)

	if err := a.Kill(); err != nil {
		t.Fatal(err)
	}
	<-a.Done()
	<-exitA

	var wg sync.WaitGroup
	wg.Go(func() { <-exitB })
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	<-b.Done()
}

func TestSpawnRejectsAMissingWorktreeAndADuplicateID(t *testing.T) {
	h := newHost(t)
	err := h.Spawn(context.Background(), "gone", runtimetest.Spec("gone", t.TempDir()+"/missing", "true"), ports.TermSize{Cols: 80, Rows: 24}, nil)
	if errs.Code(err) != "WORKSPACE_MISSING" {
		t.Fatalf("missing dir: %v, want WORKSPACE_MISSING", err)
	}

	spawn(t, h, "dup", "sleep 5")
	err = h.Spawn(context.Background(), "dup", runtimetest.Spec("dup", t.TempDir(), "true"), ports.TermSize{Cols: 80, Rows: 24}, nil)
	if errs.Code(err) != "SESSION_ALREADY_RUNNING" {
		t.Fatalf("duplicate id: %v, want SESSION_ALREADY_RUNNING", err)
	}
}

func TestTerminalConformance(t *testing.T) {
	runtimetest.TerminalConformance(t, func(t *testing.T, script string) ports.Terminal {
		term, _ := spawn(t, newHost(t), "conf", script)
		return term
	})
}

// rows splits a rendered screen or scrollback into its text rows, styles
// stripped and trailing padding trimmed.
func rows(rendered string) []string {
	if rendered == "" {
		return nil
	}
	lines := strings.Split(ansi.Strip(rendered), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// lines is a script printing n numbered lines, "line 1" to "line n", with
// format as the printf format of each.
func lines(n int, format string) string {
	return fmt.Sprintf(`i=1; while [ $i -le %d ]; do printf '%s\r\n' $i; i=$((i+1)); done; `, n, format)
}

// A client that attaches after output scrolled off the top gets that
// history in the snapshot: styled like the screen, oldest first, ending on
// the line just above the first visible row — and nothing of it again in
// the stream that follows.
func TestSnapshotCarriesScrollback(t *testing.T) {
	h := newHost(t)
	term, _ := spawn(t, h, "sb", lines(30, `\033[1mline %d\033[0m`)+`printf 'done\r\n'; read x; printf 'after\r\n'; sleep 5`)
	snap := screen(t, term, "done")

	sb := rows(snap.Scrollback)
	if len(sb) == 0 || sb[0] != "line 1" {
		t.Fatalf("scrollback rows = %q, want them to start at line 1", sb)
	}
	if strings.Contains(snap.Scrollback, "done") {
		t.Errorf("scrollback repeats the visible screen: %q", sb)
	}
	last := sb[len(sb)-1]
	first := rows(snap.Screen)[0]
	var n int
	if _, err := fmt.Sscanf(last, "line %d", &n); err != nil || first != fmt.Sprintf("line %d", n+1) {
		t.Errorf("last scrollback row %q is not just above the first screen row %q", last, first)
	}
	if !strings.Contains(snap.Scrollback, "\x1b[1m") {
		t.Errorf("scrollback lost its styling: %q", snap.Scrollback)
	}

	_, sub := term.Subscribe()
	defer sub.Close()
	_ = term.Key(ports.KeyEvent{Code: '\r'})
	var got strings.Builder
	deadline := time.After(5 * time.Second)
	for !strings.Contains(got.String(), "after") {
		select {
		case b := <-sub.C:
			got.Write(b)
		case <-deadline:
			t.Fatalf("no output after the snapshot: %q", got.String())
		}
	}
	if strings.Contains(got.String(), "line 1") {
		t.Errorf("the stream repeated scrollback from before the snapshot: %q", got.String())
	}
}

func TestScrollbackIsBoundedAtTwoThousandLines(t *testing.T) {
	h := newHost(t)
	term, _ := spawn(t, h, "big", lines(2100, `line %d`)+`printf 'done\r\n'; sleep 5`)
	snap := screen(t, term, "done")
	sb := rows(snap.Scrollback)
	if len(sb) != scrollbackLines {
		t.Fatalf("scrollback has %d rows, want %d", len(sb), scrollbackLines)
	}
	last, first := sb[len(sb)-1], rows(snap.Screen)[0]
	var n int
	if _, err := fmt.Sscanf(last, "line %d", &n); err != nil || first != fmt.Sprintf("line %d", n+1) {
		t.Errorf("after trimming, last scrollback row %q is not just above the first screen row %q", last, first)
	}
}

// The alternate screen has no history, so a snapshot taken on it omits
// scrollback; the main screen's history is still there when the program
// leaves it.
func TestAltScreenSnapshotOmitsScrollbackAndMainScreenKeepsIt(t *testing.T) {
	h := newHost(t)
	term, _ := spawn(t, h, "alt", lines(30, `line %d`)+`printf '\033[?1049h\033[Halt mode\r\n'; read x; printf '\033[?1049l'; printf 'back\r\n'; sleep 5`)
	snap := screen(t, term, "alt mode")
	if !snap.AltScreen {
		t.Fatal("snapshot is not on the alternate screen")
	}
	if snap.Scrollback != "" {
		t.Errorf("alternate screen snapshot carries scrollback: %q", rows(snap.Scrollback))
	}

	_ = term.Key(ports.KeyEvent{Code: '\r'})
	snap = screen(t, term, "back")
	if snap.AltScreen {
		t.Fatal("snapshot is still on the alternate screen")
	}
	if sb := rows(snap.Scrollback); len(sb) == 0 || sb[0] != "line 1" {
		t.Errorf("main screen lost its history over the alternate screen: %q", sb)
	}
}

// Clearing the screen keeps what was on it as history.
func TestClearScreenMovesRowsIntoScrollback(t *testing.T) {
	h := newHost(t)
	term, _ := spawn(t, h, "clr", `printf 'kept\r\n'; printf '\033[2J\033[H'; printf 'fresh\r\n'; sleep 5`)
	snap := screen(t, term, "fresh")
	if sb := rows(snap.Scrollback); len(sb) == 0 || sb[0] != "kept" {
		t.Errorf("scrollback after clear = %q, want the cleared row", sb)
	}
	if strings.Contains(ansi.Strip(snap.Screen), "kept") {
		t.Errorf("the cleared row is still on screen")
	}
}

// History keeps the width it had; a resize changes the screen, not it.
func TestScrollbackSurvivesResize(t *testing.T) {
	h := newHost(t)
	term, _ := spawn(t, h, "rsb", lines(30, `line %d`)+`printf 'done\r\n'; sleep 5`)
	screen(t, term, "done")
	if err := term.Resize(ports.TermSize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	snap, sub := term.Subscribe()
	sub.Close()
	if snap.Size != (ports.TermSize{Cols: 100, Rows: 30}) {
		t.Errorf("size = %+v, want 100x30", snap.Size)
	}
	if sb := rows(snap.Scrollback); len(sb) == 0 || sb[0] != "line 1" {
		t.Errorf("scrollback after resize = %q, want the history kept", sb)
	}
}
