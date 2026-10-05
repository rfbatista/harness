//go:build !windows

package httpclient_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/httpclient"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/application/projects"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/runtimetest"
)

// hosted is the orchestration behind the attach route, reduced to what the
// route uses: terminals from a real host, and stop.
type hosted struct {
	ports.Orchestration // nil: the attach route uses none of the rest
	host                *termhost.Host
}

func (h hosted) AttachTerminal(_ context.Context, id string) (ports.Terminal, error) {
	if id == "tui-session" {
		return nil, &domain.StructuredError{Code: "SESSION_RUNS_ON_TUI", Message: "runs in its client"}
	}
	t, err := h.host.Attach(id)
	if err != nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "session is not running"}
	}
	return t, nil
}

func (h hosted) Stop(_ context.Context, id string) error {
	if t, err := h.host.Attach(id); err == nil {
		return t.Kill()
	}
	return nil
}

var seq atomic.Int64

// remote serves a fresh host over the router and returns it and the client
// side of its terminals.
func remote(t *testing.T) (*termhost.Host, *httpclient.Terminals) {
	t.Helper()
	host := termhost.New(shell.Direct{}, ptyunix.New(), runtimetest.Script{})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	return host, httpclient.NewTerminals(serve(t, httpapi.Services{Sessions: hosted{host: host}}))
}

// spawnRemote runs script on the server's host and attaches to it over the
// WebSocket.
func spawnRemote(t *testing.T, host *termhost.Host, terms *httpclient.Terminals, script string) (string, ports.Terminal) {
	t.Helper()
	id := fmt.Sprintf("s%d", seq.Add(1))
	if err := host.Spawn(context.Background(), id, runtimetest.Spec(id, t.TempDir(), script), ports.TermSize{Cols: 80, Rows: 24}, nil); err != nil {
		t.Fatal(err)
	}
	term, err := terms.AttachTerminal(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return id, term
}

func TestTerminalOverTheWireConformance(t *testing.T) {
	runtimetest.TerminalConformance(t, func(t *testing.T, script string) ports.Terminal {
		host, terms := remote(t)
		_, term := spawnRemote(t, host, terms, script)
		return term
	})
}

func TestAttachRefusalsKeepTheirCode(t *testing.T) {
	_, terms := remote(t)
	for id, want := range map[string]string{"tui-session": "SESSION_RUNS_ON_TUI", "gone": "SESSION_NOT_RUNNING"} {
		if _, err := terms.AttachTerminal(context.Background(), id); errs.Code(err) != want {
			t.Errorf("attach %s = %v, want %s", id, err, want)
		}
	}
}

// Closing the subscription detaches: the process keeps running on the
// server, and a new attachment sees the screen it left.
func TestDetachLeavesTheProcessRunning(t *testing.T) {
	host, terms := remote(t)
	id, term := spawnRemote(t, host, terms, `printf 'left running\r\n'; cat`)
	waitScreen(t, terms, id, "left running")

	_, sub := term.Subscribe()
	sub.Close()
	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the attachment did not end on detach")
	}
	server, err := host.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.Done():
		t.Fatal("detaching stopped the process")
	default:
	}
	waitScreen(t, terms, id, "left running")
}

// A client that falls behind asks for a fresh snapshot and drops the output
// it missed; its next subscription starts from the screen as it is.
func TestFallingBehindResyncs(t *testing.T) {
	old := httpclient.SetSubscriberBuffer(2)
	defer httpclient.SetSubscriberBuffer(old)

	host, terms := remote(t)
	_, term := spawnRemote(t, host, terms, `i=0; while [ $i -lt 30 ]; do printf 'line %d\r\n' $i; sleep 0.02; i=$((i+1)); done; printf 'all done\r\n'; sleep 5`)
	_, sub := term.Subscribe()
	time.Sleep(time.Second) // not reading while the burst prints: it falls behind
	deadline := time.After(5 * time.Second)
	for closed := false; !closed; {
		select {
		case _, ok := <-sub.C:
			closed = !ok
		case <-deadline:
			t.Fatal("a subscriber that fell behind was never redrawn")
		}
	}
	// The redraw: the screen when the client asked, then everything after.
	snap, sub := term.Subscribe()
	defer sub.Close()
	seen := snap.Screen
	for !strings.Contains(ansi.Strip(seen), "all done") {
		select {
		case b := <-sub.C:
			seen += string(b)
		case <-deadline:
			t.Fatalf("redraw never caught up: %q", ansi.Strip(seen))
		}
	}
}

// waitScreen attaches anew until the snapshot shows want.
func waitScreen(t *testing.T, terms *httpclient.Terminals, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		term, err := terms.AttachTerminal(context.Background(), id)
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
		time.Sleep(20 * time.Millisecond)
	}
}

// Every channel the client opens carries the token: plain calls, the event
// feed and the terminal WebSocket.
func TestTokenOnEveryChannel(t *testing.T) {
	host := termhost.New(shell.Direct{}, ptyunix.New(), runtimetest.Script{})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	if err := host.Spawn(context.Background(), "s1", runtimetest.Spec("s1", t.TempDir(), "cat"), ports.TermSize{Cols: 80, Rows: 24}, nil); err != nil {
		t.Fatal(err)
	}
	db := openDB(t)
	projectSvc := projects.NewService(sqlite.NewProjectRepository(db), sqlite.NewRepositoryRepository(db), nil)
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: projectSvc, Sessions: hostedFeed{hosted{host: host}},
	}), httpapi.WithToken("s3cret")))
	defer srv.Close()
	ctx := context.Background()

	with := httpclient.New(srv.URL, httpclient.WithToken("s3cret"))
	if err := with.CheckAccess(ctx); err != nil {
		t.Errorf("with the token, a call = %v", err)
	}
	if _, err := httpclient.NewTerminals(with).AttachTerminal(ctx, "s1"); err != nil {
		t.Errorf("with the token, attach = %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, err := httpclient.NewEvents(with).FollowProject(cctx, "p1"); err != nil {
		t.Errorf("with the token, follow = %v", err)
	}

	without := httpclient.New(srv.URL)
	if err := without.CheckAccess(ctx); errs.Code(err) != "UNAUTHORIZED" {
		t.Errorf("without, a call = %v, want UNAUTHORIZED", err)
	}
	if _, err := httpclient.NewTerminals(without).AttachTerminal(ctx, "s1"); errs.Code(err) != "UNAUTHORIZED" {
		t.Errorf("without, attach = %v, want UNAUTHORIZED", err)
	}
	if _, err := httpclient.NewEvents(without).FollowProject(ctx, "p1"); errs.Code(err) != "UNAUTHORIZED" {
		t.Errorf("without, follow = %v, want UNAUTHORIZED", err)
	}
}

// hostedFeed adds a feed that never changes to hosted.
type hostedFeed struct{ hosted }

func (hostedFeed) FollowProject(ctx context.Context, _ string) (<-chan ports.SessionChange, error) {
	c := make(chan ports.SessionChange)
	go func() { <-ctx.Done(); close(c) }()
	return c, nil
}
