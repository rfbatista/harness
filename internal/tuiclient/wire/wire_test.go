//go:build !windows

package wire

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/httpclient"
	"operators-mcp/internal/app/runtime"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/tuiclient/tuitest"
)

func TestOptionsValidate(t *testing.T) {
	for _, runsOn := range []domain.Runner{domain.RunnerServer, domain.RunnerTUI} {
		if err := fx.ValidateApp(Options(Config{API: "http://127.0.0.1:1", Claude: "/bin/sh", RunsOn: runsOn})); err != nil {
			t.Errorf("%s: %v", runsOn, err)
		}
	}
}

func TestClientFailsStartWhenTheServerIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	app := fx.New(
		fx.Supply(Config{API: srv.URL}),
		Client,
		fx.Invoke(func(*httpclient.Client) {}),
		fx.NopLogger,
	)
	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "start the server first") {
		t.Fatalf("Start = %v, want a failure telling the user to start the server", err)
	}
}

// fakePorts binds every port the client uses to be.
func fakePorts(be *tuitest.Fake) fx.Option {
	return fx.Supply(
		fx.Annotate(be,
			fx.As(new(ports.ProjectReader)), fx.As(new(ports.RepositoryCatalog)),
			fx.As(new(ports.TicketBoard)), fx.As(new(ports.AgentCatalog)),
			fx.As(new(ports.SessionReader)), fx.As(new(ports.InteractiveSessions)),
			fx.As(new(ports.TerminalAccess)), fx.As(new(ports.SessionFeed)),
		),
	)
}

func TestOrphansStopsInteractiveSessionsLeftRunning(t *testing.T) {
	be := &tuitest.Fake{Sessions: []*domain.Session{
		{ID: "left", Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerTUI, RunnerHost: "laptop"},
		{ID: "legacy", Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerTUI},
		{ID: "elsewhere", Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerTUI, RunnerHost: "desktop"},
		{ID: "hosted", Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerServer},
		{ID: "headless", Status: domain.SessionRunning},
		{ID: "ended", Status: domain.SessionDone, Interactive: true, RunsOn: domain.RunnerTUI},
	}}
	stopped := &OrphansStopped{}
	app := fx.New(fakePorts(be), fx.Supply(stopped, Config{RunnerHost: "laptop"}), Orphans, fx.NopLogger)
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Stop(context.Background()) }()

	want := []tuitest.EndCall{{SessionID: "left", Closed: true}, {SessionID: "legacy", Closed: true}}
	if got := be.EndCalls(); !slices.Equal(got, want) {
		t.Fatalf("EndInteractive calls = %+v, want this machine's running tui sessions, closed: %+v", got, want)
	}
	if stopped.N != 2 {
		t.Fatalf("OrphansStopped = %d, want 2", stopped.N)
	}
}

// programApp runs Program over be, reading keys from in and drawing nowhere.
func programApp(be *tuitest.Fake, in io.Reader) *fx.App {
	return fx.New(
		fakePorts(be),
		fx.Supply(&OrphansStopped{}, runtime.Config{}, Config{RunsOn: domain.RunnerTUI}),
		runtime.Module,
		fx.Supply(
			fx.Annotate(tea.WithInput(in), fx.ResultTags(`group:"tea_options"`)),
			fx.Annotate(tea.WithOutput(io.Discard), fx.ResultTags(`group:"tea_options"`)),
			fx.Annotate(tea.WithWindowSize(80, 24), fx.ResultTags(`group:"tea_options"`)),
		),
		Program,
		fx.NopLogger,
	)
}

func TestProgramQuitShutsTheAppDown(t *testing.T) {
	be := &tuitest.Fake{Projects: []*domain.Project{{ID: "p1", Name: "api"}}}
	app := programApp(be, strings.NewReader("q"))
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case sig := <-app.Wait():
		if sig.ExitCode != 0 {
			t.Errorf("exit code = %d, want 0", sig.ExitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pressing q did not shut the app down")
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStoppingTheAppStopsTheProgram(t *testing.T) {
	pr, pw := io.Pipe() // no keys: the program runs until stopped
	defer pw.Close()
	app := programApp(&tuitest.Fake{}, pr)
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop = %v, want the program to quit", err)
	}
}

func TestClientFailsStartWithoutTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"a valid bearer token is required","code":"UNAUTHORIZED"}`))
	}))
	defer srv.Close()

	app := fx.New(fx.Supply(Config{API: srv.URL}), Client, fx.Invoke(func(*httpclient.Client) {}), fx.NopLogger)
	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "wants a token") {
		t.Fatalf("Start = %v, want a failure asking for the token", err)
	}
}
