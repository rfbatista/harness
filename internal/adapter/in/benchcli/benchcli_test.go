package benchcli

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcpbench"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// benchSessions stands in for the CLI: every run answers a fixed result, after
// gate is open when one is set. No process, no model.
type benchSessions struct {
	gate chan struct{}
	mu   sync.Mutex
	runs int
}

func (s *benchSessions) Setup(context.Context, ports.BenchSetupRequest) (ports.BenchSetupInfo, error) {
	return ports.BenchSetupInfo{Model: "sonnet", AgentPrompt: "p"}, nil
}

func (s *benchSessions) Run(ctx context.Context, _ ports.BenchSessionRequest) (ports.BenchSessionResult, error) {
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return ports.BenchSessionResult{}, ctx.Err()
		}
	}
	s.mu.Lock()
	s.runs++
	s.mu.Unlock()
	return ports.BenchSessionResult{FinalText: "done", CostUSD: 0.01}, nil
}

type judge struct{}

func (judge) Ask(context.Context, string, string, string) (string, error) {
	return `{"similarity": 1, "score": 1, "steps": []}`, nil
}

type projects map[string]bool

func (p projects) Get(_ context.Context, id string) *domain.Project {
	if p[id] {
		return &domain.Project{ID: id}
	}
	return nil
}

func testServer(t *testing.T, sessions *benchSessions, withCase bool) (*httptest.Server, *bench.Service) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := bench.New(bench.Deps{
		Cases: sqlite.NewBenchCaseRepository(db), Scorecards: sqlite.NewBenchScorecardRepository(db),
		Projects: projects{"p1": true}, Sessions: sessions, ServeMock: mcpbench.Serve, MockName: mcpbench.Name, Judge: judge{},
	})
	if withCase {
		_, err := svc.CreateCase(context.Background(), "p1", domain.BenchCase{
			Name: "reads the readme", Prompt: "Read it.", Tools: []domain.BenchTool{{Name: "read_file", Result: "x"}},
			Expect: domain.BenchExpectation{Reference: "done"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Bench: svc, BenchRuns: svc}), httpapi.WithToken("secret")))
	t.Cleanup(srv.Close)
	return srv, svc
}

func run(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Run(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestTheCommandRunsTheBenchAndPrintsPerCaseAndAggregateScores(t *testing.T) {
	srv, _ := testServer(t, &benchSessions{}, true)

	code, out, errOut := run(t, "run", "-url", srv.URL, "-token", "secret", "-project", "p1", "-poll", "5ms")
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	for _, want := range []string{"reads the readme", "outcome", "aggregate", "cost", "no previous run"} {
		if !strings.Contains(out, want) {
			t.Errorf("the summary lacks %q:\n%s", want, out)
		}
	}

	// A second run prints the difference to the first.
	code, out, _ = run(t, "run", "-url", srv.URL, "-token", "secret", "-project", "p1", "-poll", "5ms")
	if code != 0 || !strings.Contains(out, "previous run") || strings.Contains(out, "no previous run") || !strings.Contains(out, "unchanged") {
		t.Fatalf("exit %d, second summary:\n%s", code, out)
	}
}

func TestTheCommandFailsWithTheServersMessage(t *testing.T) {
	gate := make(chan struct{})
	srv, svc := testServer(t, &benchSessions{gate: gate}, true)
	t.Cleanup(func() { close(gate) })

	if code, _, errOut := run(t, "run", "-url", srv.URL, "-token", "secret", "-project", "nope"); code == 0 || !strings.Contains(errOut, "project not found") {
		t.Fatalf("unknown project: exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := run(t, "run", "-url", srv.URL, "-token", "wrong", "-project", "p1"); code == 0 || !strings.Contains(errOut, "bearer token") {
		t.Fatalf("bad token: exit %d, stderr %q", code, errOut)
	}

	// A concurrent run: one is held open, the command's own is refused.
	if _, err := svc.StartRun(context.Background(), "p1", domain.BenchRunRequest{}); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run(t, "run", "-url", srv.URL, "-token", "secret", "-project", "p1"); code == 0 || !strings.Contains(errOut, "already has a bench run") {
		t.Fatalf("concurrent: exit %d, stderr %q", code, errOut)
	}
}

func TestTheCommandOnAProjectWithNoCasesFailsWithTheServersMessage(t *testing.T) {
	srv, _ := testServer(t, &benchSessions{}, false)
	if code, _, errOut := run(t, "run", "-url", srv.URL, "-token", "secret", "-project", "p1"); code == 0 || !strings.Contains(errOut, "no bench cases") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestTheCommandRefusesMissingArguments(t *testing.T) {
	if code, _, errOut := run(t, "run"); code != 2 || !strings.Contains(errOut, "-project") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if code, _, _ := run(t, "nonsense"); code != 2 {
		t.Fatalf("exit %d for an unknown subcommand", code)
	}
}
