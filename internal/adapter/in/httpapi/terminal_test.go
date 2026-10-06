//go:build !windows

package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/coder/websocket"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/runtimetest"
)

// hostedSessions is the orchestration behind the session terminal route,
// reduced to attaching: terminals come from a real host.
type hostedSessions struct {
	ports.Orchestration // nil: the route uses nothing else
	host                *termhost.Host
}

func (h hostedSessions) AttachTerminal(_ context.Context, id string) (ports.Terminal, error) {
	t, err := h.host.Attach(id)
	if err != nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "session is not running"}
	}
	return t, nil
}

// firstSnapshot dials the terminal socket and returns the snapshot it opens with.
func firstSnapshot(t *testing.T, url string) ports.TerminalSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m ports.TerminalMessage
	if err := json.Unmarshal(data, &m); err != nil || m.Type != "snapshot" || m.Snapshot == nil {
		t.Fatalf("first frame = %s (%v), want a snapshot", data, err)
	}
	return *m.Snapshot
}

// The session terminal's first snapshot carries the history that scrolled
// off before the client attached.
func TestHTTP_SessionTerminalSnapshotCarriesScrollback(t *testing.T) {
	host := termhost.New(shell.Direct{}, ptyunix.New(), runtimetest.Script{})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	srv := httptest.NewServer(NewRouter(NewHandler(Services{Sessions: hostedSessions{host: host}})))
	defer srv.Close()

	script := `i=1; while [ $i -le 30 ]; do printf 'line %d\r\n' $i; i=$((i+1)); done; printf 'done\r\n'; sleep 5`
	if err := host.Spawn(context.Background(), "s1", runtimetest.Spec("s1", t.TempDir(), script), ports.TermSize{Cols: 80, Rows: 24}, nil); err != nil {
		t.Fatal(err)
	}
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/sessions/s1/terminal"
	deadline := time.Now().Add(5 * time.Second)
	var snap ports.TerminalSnapshot
	for !strings.Contains(ansi.Strip(snap.Screen), "done") {
		if time.Now().After(deadline) {
			t.Fatalf("the screen never showed the end of the output: %q", ansi.Strip(snap.Screen))
		}
		snap = firstSnapshot(t, url)
		time.Sleep(20 * time.Millisecond)
	}
	sb := strings.Split(ansi.Strip(snap.Scrollback), "\n")
	if len(sb) == 0 || strings.TrimRight(sb[0], " ") != "line 1" {
		t.Fatalf("scrollback over the wire = %q, want it to start at line 1", sb)
	}
	if snap.Log {
		t.Error("a session snapshot was marked as a log")
	}
}
