package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// titlePoll is how often the attach loop checks for a new window title.
const titlePoll = 500 * time.Millisecond

// handleSessionTerminal attaches a client to a RunnerServer session's
// terminal over a WebSocket, speaking ports.TerminalMessage. The session is
// checked before the upgrade, so a refusal is the usual {"error", "code"}
// response. Several clients may attach to one terminal at once; closing the
// socket detaches without stopping the session.
func (h *Handler) handleSessionTerminal(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	t, err := h.orchSvc.AttachTerminal(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return serveTerminal(c, t)
}

// handleRunTerminal attaches a client to an application run's terminal, with
// the same protocol as a session's; the first snapshot replays the run's
// output so far.
func (h *Handler) handleRunTerminal(c echo.Context) error {
	if h.apps == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "application runs not configured")
	}
	t, err := h.apps.Attach(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return serveTerminal(c, t)
}

// serveTerminal upgrades to a WebSocket speaking ports.TerminalMessage over t.
func serveTerminal(c echo.Context, t ports.Terminal) error {
	conn, err := websocket.Accept(c.Response(), c.Request(), nil)
	if err != nil {
		return nil // Accept has answered the request
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()
	resync := make(chan struct{}, 1)
	go func() {
		defer cancel()
		readInput(ctx, conn, t, resync)
	}()
	streamOutput(ctx, conn, t, resync)
	return nil
}

// readInput applies the client's keys, pastes and resizes until the socket
// closes, and passes on its requests for a fresh snapshot.
func readInput(ctx context.Context, conn *websocket.Conn, t ports.Terminal, resync chan<- struct{}) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var m ports.TerminalMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch {
		case m.Type == "key" && m.Key != nil:
			_ = t.Key(*m.Key)
		case m.Type == "paste":
			_ = t.Paste(m.Text)
		case m.Type == "resize" && m.Size != nil:
			_ = t.Resize(*m.Size)
		case m.Type == "resync":
			select {
			case resync <- struct{}{}:
			default: // one is already due
			}
		}
	}
}

// streamOutput sends the snapshot, then the output, titles and the exit, from
// this one goroutine. A subscription dropped for falling behind — here, or in
// the client, which asks with resync — is replaced by a fresh one, whose
// snapshot redraws the client's screen, history included.
func streamOutput(ctx context.Context, conn *websocket.Conn, t ports.Terminal, resync <-chan struct{}) {
	snap, sub := t.Subscribe()
	defer func() { sub.Close() }()
	if send(ctx, conn, ports.TerminalMessage{Type: "snapshot", Snapshot: &snap}) != nil {
		return
	}
	title := snap.Title
	tick := time.NewTicker(titlePoll)
	defer tick.Stop()
	for {
		select {
		case b, ok := <-sub.C:
			if ok {
				if conn.Write(ctx, websocket.MessageBinary, b) != nil {
					return
				}
				continue
			}
			select {
			case <-t.Done():
				_ = send(ctx, conn, ports.TerminalMessage{Type: "exit", Code: t.ExitCode()})
				_ = conn.Close(websocket.StatusNormalClosure, "exited")
				return
			default:
				snap, sub = t.Subscribe()
				if send(ctx, conn, ports.TerminalMessage{Type: "snapshot", Snapshot: &snap}) != nil {
					return
				}
			}
		case <-resync:
			sub.Close()
			snap, sub = t.Subscribe()
			if send(ctx, conn, ports.TerminalMessage{Type: "snapshot", Snapshot: &snap}) != nil {
				return
			}
		case <-tick.C:
			if now := t.Title(); now != title {
				title = now
				if send(ctx, conn, ports.TerminalMessage{Type: "title", Title: now}) != nil {
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func send(ctx context.Context, conn *websocket.Conn, m ports.TerminalMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}
