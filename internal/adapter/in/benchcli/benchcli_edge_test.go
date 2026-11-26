package benchcli

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcpbench"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Seam C, the bench command: what it prints and which exit code it gives when
// a run does not go the plain way.

// lockedBuffer is written by the command while the test reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestTheCommandOnARunCancelledFromOutsidePrintsItsScorecardAndExits1(t *testing.T) {
	gate := make(chan struct{})
	srv, svc := testServer(t, &benchSessions{gate: gate}, true)
	t.Cleanup(func() { close(gate) })

	var out, errOut lockedBuffer
	done := make(chan int, 1)
	go func() {
		done <- Run(context.Background(), []string{"run", "-url", srv.URL, "-token", "secret", "-project", "p1", "-poll", "5ms"}, &out, &errOut)
	}()
	var id string
	for end := time.Now().Add(10 * time.Second); id == "" && time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if m := regexp.MustCompile(`bench run (\w+) started`).FindStringSubmatch(out.String()); m != nil {
			id = m[1]
		}
	}
	if id == "" {
		t.Fatalf("the command never reported its run: %q", out.String())
	}
	if _, err := svc.CancelRun(context.Background(), "p1", id); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 1 || !strings.Contains(errOut.String(), "cancelled") || !strings.Contains(out.String(), "(cancelled)") {
			t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command did not end after the cancel")
	}
}

func TestTheCommandOnAServerNobodyListensOnFailsAsUnreachable(t *testing.T) {
	code, _, errOut := run(t, "run", "-url", "http://127.0.0.1:1", "-project", "p1")
	if code != 1 || !strings.Contains(errOut, "not reachable") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

func TestTheCommandStopsWhenItsContextEndsWhileARunIsGoingOn(t *testing.T) {
	gate := make(chan struct{})
	srv, _ := testServer(t, &benchSessions{gate: gate}, true)
	t.Cleanup(func() { close(gate) })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var o, e bytes.Buffer
	code := Run(ctx, []string{"run", "-url", srv.URL, "-token", "secret", "-project", "p1", "-poll", "10ms"}, &o, &e)
	if code != 1 || e.Len() == 0 {
		t.Fatalf("exit %d stderr %q", code, e.String())
	}
}

func TestTheCommandReadsTheServerFromTheEnvironmentAndIgnoresATrailingSlash(t *testing.T) {
	srv, _ := testServer(t, &benchSessions{}, true)
	t.Setenv("HARNESS_URL", srv.URL+"/")
	code, out, errOut := run(t, "run", "-token", "secret", "-project", "p1", "-poll", "5ms")
	if code != 0 || !strings.Contains(out, "aggregate") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
}

func TestTheCommandRefusesAnUnknownFlagAndAMissingSubcommand(t *testing.T) {
	if code, _, _ := run(t, "run", "-bogus"); code != 2 {
		t.Fatalf("unknown flag: exit %d, want 2", code)
	}
	if code, _, errOut := run(t); code != 2 || !strings.Contains(errOut, "usage") {
		t.Fatalf("no subcommand: exit %d stderr %q", code, errOut)
	}
}

// failingSessions makes every case fail to run, as a CLI that dies would.
type failingSessions struct{}

func (failingSessions) Setup(context.Context, ports.BenchSetupRequest) (ports.BenchSetupInfo, error) {
	return ports.BenchSetupInfo{Model: "sonnet"}, nil
}
func (failingSessions) Run(context.Context, ports.BenchSessionRequest) (ports.BenchSessionResult, error) {
	return ports.BenchSessionResult{}, errors.New("the CLI ended before it reported a result")
}

func TestTheCommandNamesACaseThatDidNotRunAndStillExits0ForAFinishedRun(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := bench.New(bench.Deps{Cases: sqlite.NewBenchCaseRepository(db), Scorecards: sqlite.NewBenchScorecardRepository(db),
		Projects: projects{"p1": true}, Sessions: failingSessions{}, ServeMock: mcpbench.Serve, MockName: mcpbench.Name, Judge: judge{}})
	if _, err := svc.CreateCase(context.Background(), "p1", domain.BenchCase{Name: "doomed", Prompt: "p", Expect: domain.BenchExpectation{Reference: "r"}}); err != nil {
		t.Fatal(err)
	}
	srv := serve(t, svc)
	code, out, errOut := run(t, "run", "-url", srv.URL, "-token", "secret", "-project", "p1", "-poll", "5ms")
	if !strings.Contains(out, "did not run") || !strings.Contains(out, "doomed") {
		t.Fatalf("the failed case is not named:\n%s", out)
	}
	// The run itself finished (the case failed, not the run): a scorecard exists and the exit is 0.
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

func serve(t *testing.T, svc *bench.Service) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Bench: svc, BenchRuns: svc}), httpapi.WithToken("secret")))
	t.Cleanup(srv.Close)
	return srv
}

// Ctrl-C ends the command's context: the run it started on the server is
// cancelled too, not left going with nobody watching it.
func TestTheCommandCancelsItsRunOnTheServerWhenItIsInterrupted(t *testing.T) {
	gate := make(chan struct{})
	srv, svc := testServer(t, &benchSessions{gate: gate}, true)
	t.Cleanup(func() { close(gate) })

	ctx, interrupt := context.WithCancel(context.Background())
	var out, errOut lockedBuffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"run", "-url", srv.URL, "-token", "secret", "-project", "p1", "-poll", "5ms"}, &out, &errOut)
	}()
	var id string
	for end := time.Now().Add(10 * time.Second); id == "" && time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if m := regexp.MustCompile(`bench run (\w+) started`).FindStringSubmatch(out.String()); m != nil {
			id = m[1]
		}
	}
	if id == "" {
		t.Fatalf("the command never reported its run: %q", out.String())
	}
	interrupt()
	select {
	case code := <-done:
		if code != 1 || !strings.Contains(errOut.String(), "interrupted") {
			t.Fatalf("exit %d stderr %q", code, errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command did not end after the interrupt")
	}
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if r, err := svc.GetRun(context.Background(), "p1", id); err == nil && r.Status == domain.BenchRunCancelled {
			return
		}
	}
	r, _ := svc.GetRun(context.Background(), "p1", id)
	t.Fatalf("the run on the server was left %s", r.Status)
}
