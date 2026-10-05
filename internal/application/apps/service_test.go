package apps_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/agents/command"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type sessions map[string]*domain.Session

func (s sessions) Get(_ context.Context, id string) (*domain.Session, error) {
	if x, ok := s[id]; ok {
		return x, nil
	}
	return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
}

func (s sessions) List(context.Context, ports.SessionFilter) ([]*domain.Session, error) {
	return nil, nil
}

type commands map[string][]*domain.RunCommand

func (c commands) ListRunCommands(_ context.Context, repositoryID string) ([]*domain.RunCommand, error) {
	return c[repositoryID], nil
}

// newRunner runs commands for real: a PTY, the plain shell, the command agent.
func newRunner(t *testing.T) (*apps.Service, string) {
	t.Helper()
	worktree := t.TempDir()
	host := termhost.New(shell.Direct{}, ptyunix.New(), command.Agent{Shell: "/bin/sh"})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	svc := apps.NewService(
		sessions{"s1": {ID: "s1", RepositoryID: "r1", WorkingDir: worktree}, "bare": {ID: "bare"}},
		commands{"r1": {{RepositoryID: "r1", Name: "greet", Command: `printf 'hello from %s\n' "$(basename "$PWD")"; sleep 30`}}},
		host,
	)
	return svc, worktree
}

// readAll attaches and collects what the terminal shows for a moment.
func readUntil(t *testing.T, svc *apps.Service, runID, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		term, err := svc.Attach(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		snap, sub := term.Subscribe()
		sub.Close()
		if strings.Contains(snap.Screen, want) {
			return snap.Screen
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run never printed %q", want)
	return ""
}

func TestASavedCommandRunsInTheSessionsWorktreeAndKeepsItsLog(t *testing.T) {
	svc, worktree := newRunner(t)
	ctx := context.Background()

	r, err := svc.Start(ctx, "s1", "greet", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != domain.AppRunRunning || r.Dir != worktree || r.Name != "greet" {
		t.Fatalf("run = %+v", r)
	}
	// A later attach (another viewer, or after a reload) replays the output.
	screen := readUntil(t, svc, r.ID, "hello from "+lastSegment(worktree))
	if !strings.Contains(screen, "hello from") {
		t.Fatalf("replay = %q", screen)
	}

	again, err := svc.Start(ctx, "s1", "greet", "")
	if err != nil || again.ID != r.ID {
		t.Fatalf("starting a running saved command returns that run: %+v %v", again, err)
	}

	stopped, err := svc.Stop(ctx, r.ID)
	if err != nil || stopped.Status != domain.AppRunStopped {
		t.Fatalf("stop: %+v %v", stopped, err)
	}
	runs, _ := svc.List(ctx, "s1")
	if len(runs) != 1 || runs[0].Status != domain.AppRunStopped || runs[0].EndedAt == nil {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestAnAdHocCommandRunsAndItsExitIsRecorded(t *testing.T) {
	svc, _ := newRunner(t)
	ctx := context.Background()
	r, err := svc.Start(ctx, "s1", "", "echo done; exit 3")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "echo done; exit 3" {
		t.Errorf("an ad-hoc run is named by its command: %q", r.Name)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ := svc.List(ctx, "s1")
		if runs[0].Status == domain.AppRunExited {
			if runs[0].ExitCode != 3 {
				t.Fatalf("exit code = %d", runs[0].ExitCode)
			}
			readUntil(t, svc, r.ID, "done")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the run never exited")
}

func TestStartRefusals(t *testing.T) {
	svc, _ := newRunner(t)
	ctx := context.Background()
	for _, c := range []struct{ session, name, command, code string }{
		{"ghost", "greet", "", "SESSION_NOT_FOUND"},
		{"bare", "", "make air", "INVALID_INPUT"}, // no worktree
		{"s1", "nope", "", "RUN_COMMAND_NOT_FOUND"},
		{"s1", "", "  ", "INVALID_INPUT"},
	} {
		if _, err := svc.Start(ctx, c.session, c.name, c.command); errs.Code(err) != c.code {
			t.Errorf("%+v: got %v, want %s", c, err, c.code)
		}
	}
	if _, err := svc.Stop(ctx, "run-nope"); errs.Code(err) != "RUN_NOT_FOUND" {
		t.Errorf("stop unknown: %v", err)
	}
	if _, err := svc.Attach(ctx, "run-nope"); errs.Code(err) != "RUN_NOT_FOUND" {
		t.Errorf("attach unknown: %v", err)
	}
}

func lastSegment(p string) string {
	return p[strings.LastIndex(p, "/")+1:]
}
