//go:build !windows

package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// scriptAgent stands in for the claude CLI on the server's terminal host: it
// runs a shell script in the session's worktree.
type scriptAgent struct {
	script string
	err    error
}

func (scriptAgent) Kind() string { return "claude" }

func (a scriptAgent) Command(spec ports.AgentSpec) (ports.ShellCommand, error) {
	if a.err != nil {
		return ports.ShellCommand{}, a.err
	}
	return ports.ShellCommand{Program: "/bin/sh", Args: []string{"-c", a.script}, Dir: spec.Dir, Env: spec.Env}, nil
}

// newHostedService is the interactive service with a terminal host running
// script in place of claude.
func newHostedService(t *testing.T, agent scriptAgent) *Service {
	t.Helper()
	svc, _ := newInteractiveService(t)
	host := termhost.New(shell.Direct{}, ptyunix.New(), agent)
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	svc.Terminals = host
	return svc
}

func startOnServer(t *testing.T, svc *Service) *domain.Session {
	t.Helper()
	sess, _ := startInteractive(t, svc, InteractiveRequest{RunsOn: domain.RunnerServer, RunnerHost: "laptop", Size: ports.TermSize{Cols: 80, Rows: 24}})
	return sess
}

func waitStatus(t *testing.T, svc *Service, id string, want domain.SessionStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := sessionOf(svc, id)
		if got != nil && got.Status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %+v, want %s", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func screenOf(t *testing.T, svc *Service, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		term, err := svc.AttachTerminal(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		snap, sub := term.Subscribe()
		sub.Close()
		if strings.Contains(ansi.Strip(snap.Screen), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("screen never showed %q: %q", want, ansi.Strip(snap.Screen))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerSession_RunsOnTheHostAndEndsWithIt(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `printf 'hosted\r\n'; read x; exit 2`})
	sess := startOnServer(t, svc)
	if sess.RunsOn != domain.RunnerServer || sess.RunnerHost != "" {
		t.Fatalf("recorded as %q on %q, want server with no client host", sess.RunsOn, sess.RunnerHost)
	}
	screenOf(t, svc, sess.ID, "hosted")

	term, _ := svc.AttachTerminal(context.Background(), sess.ID)
	_ = term.Key(ports.KeyEvent{Code: '\r'})
	waitStatus(t, svc, sess.ID, domain.SessionFailed)

	if _, err := svc.AttachTerminal(context.Background(), sess.ID); codeOf(err) != "SESSION_NOT_RUNNING" {
		t.Errorf("attach after exit = %v, want SESSION_NOT_RUNNING", err)
	}
}

func TestServerSession_StopRecordsStoppedNotFailed(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `printf 'up\r\n'; cat`})
	sess := startOnServer(t, svc)
	screenOf(t, svc, sess.ID, "up")

	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, svc, sess.ID, domain.SessionStopped)
	if _, err := svc.Terminals.Attach(sess.ID); err == nil {
		t.Error("the terminal outlived Stop")
	}
}

func TestServerSession_TheClientCannotEndIt(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	sess := startOnServer(t, svc)
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, true); codeOf(err) != "SESSION_RUNS_ON_SERVER" {
		t.Fatalf("EndInteractive = %v, want SESSION_RUNS_ON_SERVER", err)
	}
	if got := sessionOf(svc, sess.ID); got.Status != domain.SessionRunning {
		t.Errorf("status = %s, want still running", got.Status)
	}
}

func TestTUISession_RecordsItsHostAndCannotBeAttached(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	sess, _ := startInteractive(t, svc, InteractiveRequest{RunnerHost: "laptop"})
	if sess.RunsOn != domain.RunnerTUI || sess.RunnerHost != "laptop" {
		t.Fatalf("recorded as %q on %q, want tui on laptop", sess.RunsOn, sess.RunnerHost)
	}
	if _, err := svc.AttachTerminal(context.Background(), sess.ID); codeOf(err) != "SESSION_RUNS_ON_TUI" {
		t.Errorf("attach = %v, want SESSION_RUNS_ON_TUI", err)
	}
	if _, err := svc.Terminals.Attach(sess.ID); err == nil {
		t.Error("the server spawned a session the client runs")
	}
}

func TestRunsOn_Validation(t *testing.T) {
	svc, _ := newInteractiveService(t) // no terminal host
	req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1"}

	req.RunsOn = domain.RunnerServer
	if _, _, err := svc.StartInteractive(context.Background(), req); codeOf(err) != "SERVER_HOSTING_UNAVAILABLE" {
		t.Errorf("server without a host = %v, want SERVER_HOSTING_UNAVAILABLE", err)
	}
	req.RunsOn = "moon"
	if _, _, err := svc.StartInteractive(context.Background(), req); codeOf(err) != "INVALID_INPUT" {
		t.Errorf("runs_on moon = %v, want INVALID_INPUT", err)
	}
}

func TestResume_MovesASessionToTheServer(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `printf 'moved\r\n'; cat`})
	sess, _ := startInteractive(t, svc, InteractiveRequest{RunnerHost: "laptop"})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID, RunsOn: domain.RunnerServer})
	if err != nil {
		t.Fatal(err)
	}
	if got.RunsOn != domain.RunnerServer || got.RunnerHost != "" || got.Status != domain.SessionRunning {
		t.Fatalf("resumed as %+v, want running on the server", got)
	}
	screenOf(t, svc, sess.ID, "moved")
}

func TestSpawnFailureEndsTheSession(t *testing.T) {
	svc := newHostedService(t, scriptAgent{err: errors.New("no agent")})
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", RunsOn: domain.RunnerServer,
	})
	if err == nil {
		t.Fatal("StartInteractive succeeded with an agent that cannot start")
	}
	list, _ := svc.List(context.Background(), ports.SessionFilter{})
	if len(list) != 1 || list[0].Status != domain.SessionFailed {
		t.Fatalf("sessions = %+v, want the one recorded as failed", list)
	}
}

// At boot, server sessions recorded as running have no terminal: the previous
// server's processes are gone. Live ones are left alone.
func TestStopOrphanedServerSessions(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	live := startOnServer(t, svc)
	orphan, err := svc.sessions.Create(&domain.Session{
		ID: "orphan", ProjectID: "p1", Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerServer,
	})
	if err != nil {
		t.Fatal(err)
	}
	tui, _ := startInteractive(t, svc, InteractiveRequest{})

	n, err := svc.StopOrphanedServerSessions(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("StopOrphanedServerSessions = %d, %v; want 1", n, err)
	}
	for id, want := range map[string]domain.SessionStatus{
		orphan.ID: domain.SessionStopped, live.ID: domain.SessionRunning, tui.ID: domain.SessionRunning,
	} {
		if got := sessionOf(svc, id).Status; got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
}

// On shutdown the server stops its sessions and records them as stopped,
// rather than letting the kill read as a failure.
func TestStopServerSessions(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	sess := startOnServer(t, svc)
	svc.StopServerSessions(context.Background())
	waitStatus(t, svc, sess.ID, domain.SessionStopped)
}

// Two people clicking Resume at once: one agent process starts, and the
// session it runs in is not ended by the call that lost.
func TestResume_ConcurrentCallsSpawnOnce(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `printf 'back\r\n'; cat`})
	tr := &gatedTranscripts{}
	svc.Transcripts = tr
	sess := startOnServer(t, svc)
	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, svc, sess.ID, domain.SessionStopped)

	ok, codes := resumeAtOnce(t, svc, tr, sess, ports.ResumeRequest{RunsOn: domain.RunnerServer}, 4)
	if ok != 1 {
		t.Fatalf("%d resumes succeeded, want 1 (others: %v)", ok, codes)
	}
	time.Sleep(100 * time.Millisecond)
	if got := sessionOf(svc, sess.ID); got.Status != domain.SessionRunning {
		t.Fatalf("status = %s, want running", got.Status)
	}
	screenOf(t, svc, sess.ID, "back")
}
