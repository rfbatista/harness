package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"operators-mcp/internal/ports"
)

var _ ports.TerminalAccess = (*Terminals)(nil)

// Terminals is ports.TerminalAccess over the server's attach WebSocket: the
// terminal of a session the server runs, drawn and typed into from here.
type Terminals struct{ c *Client }

// NewTerminals returns the terminal adapter over c.
func NewTerminals(c *Client) *Terminals { return &Terminals{c: c} }

// AttachTerminal opens the session's terminal. A refusal comes back with its
// code, like any other call.
func (a *Terminals) AttachTerminal(ctx context.Context, sessionID string) (ports.Terminal, error) {
	u, err := url.Parse(a.c.base + "/api/sessions/" + url.PathEscape(sessionID) + "/terminal")
	if err != nil {
		return nil, err
	}
	u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
	header := http.Header{}
	if a.c.token != "" {
		header.Set("Authorization", "Bearer "+a.c.token)
	}
	conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil && resp.StatusCode >= 300 {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			return nil, decodeError(resp.StatusCode, body)
		}
		return nil, fmt.Errorf("coding_pool server at %s: %w", a.c.base, err)
	}
	conn.SetReadLimit(64 << 20)

	first, err := readMessage(ctx, conn)
	if err != nil || first.Type != "snapshot" || first.Snapshot == nil {
		conn.CloseNow()
		return nil, fmt.Errorf("attach %s: no snapshot from the server", sessionID)
	}
	t := &remoteTerminal{
		client: a.c,
		id:     sessionID,
		conn:   conn,
		snap:   *first.Snapshot,
		title:  first.Snapshot.Title,
		done:   make(chan struct{}),
	}
	go t.read()
	return t, nil
}

func readMessage(ctx context.Context, conn *websocket.Conn) (ports.TerminalMessage, error) {
	var m ports.TerminalMessage
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return m, err
	}
	if typ != websocket.MessageText {
		return m, fmt.Errorf("unexpected binary frame")
	}
	return m, json.Unmarshal(data, &m)
}

// subscriberBuffer is how many output chunks the pane may fall behind before
// the terminal asks the server to redraw it from a fresh snapshot.
var subscriberBuffer = 1024

// remoteTerminal is one attachment. Output that arrives while no one is
// subscribed is kept, so a subscription always starts at the snapshot and
// misses nothing after it.
type remoteTerminal struct {
	client *Client
	id     string
	conn   *websocket.Conn

	mu      sync.Mutex
	snap    ports.TerminalSnapshot
	pending [][]byte    // output since snap, not yet handed to a subscriber
	sub     chan []byte // the current subscriber, or nil
	lagged  bool        // dropping output until the server's next snapshot
	title   string
	code    int
	done    chan struct{}
}

// read applies the server's frames until the socket closes. Losing the
// connection reads as the process gone, with code -1: from here nothing more
// can be seen of it.
func (t *remoteTerminal) read() {
	code := -1
	defer func() { t.finish(code) }()
	for {
		typ, data, err := t.conn.Read(context.Background())
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			t.output(data)
			continue
		}
		var m ports.TerminalMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "snapshot":
			if m.Snapshot != nil {
				t.resnapshot(*m.Snapshot)
			}
		case "title":
			t.mu.Lock()
			t.title = m.Title
			t.mu.Unlock()
		case "exit":
			code = m.Code
			return
		}
	}
}

func (t *remoteTerminal) output(b []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case t.lagged:
	case t.sub == nil:
		t.pending = append(t.pending, b)
	default:
		select {
		case t.sub <- b:
		default:
			// Too far behind: what the pane missed is gone from this stream,
			// so ask for the screen as it is and drop output until it comes.
			t.lagged = true
			go func() { _ = t.write(ports.TerminalMessage{Type: "resync"}) }()
		}
	}
}

// resnapshot takes a fresh screen. The current subscriber is closed, so the
// pane subscribes again and redraws from it.
func (t *remoteTerminal) resnapshot(s ports.TerminalSnapshot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.snap, t.pending, t.lagged = s, nil, false
	if s.Title != "" {
		t.title = s.Title
	}
	if t.sub != nil {
		close(t.sub)
		t.sub = nil
	}
}

func (t *remoteTerminal) finish(code int) {
	t.conn.CloseNow()
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.done:
		return
	default:
	}
	t.code = code
	close(t.done) // before the subscription, so a closed one reads as exited
	if t.sub != nil {
		close(t.sub)
		t.sub = nil
	}
}

func (t *remoteTerminal) Subscribe() (ports.TerminalSnapshot, ports.Subscription) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := make(chan []byte, subscriberBuffer+len(t.pending))
	for _, b := range t.pending {
		c <- b
	}
	t.pending = nil
	if t.sub != nil {
		close(t.sub)
	}
	t.sub = c
	select {
	case <-t.done:
		close(c)
		t.sub = nil
	default:
	}
	snap := t.snap
	snap.Title = t.title
	return snap, ports.Subscription{C: c, Close: func() { t.unsubscribe(c) }}
}

// unsubscribe ends the current subscription, and with it the attachment: a
// remote terminal has one viewer. Closing a subscription already replaced
// changes nothing.
func (t *remoteTerminal) unsubscribe(c chan []byte) {
	t.mu.Lock()
	current := t.sub == c
	if current {
		close(c)
		t.sub = nil
	}
	t.mu.Unlock()
	if current {
		_ = t.conn.Close(websocket.StatusNormalClosure, "detached")
	}
}

func (t *remoteTerminal) write(m ports.TerminalMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return t.conn.Write(ctx, websocket.MessageText, b)
}

func (t *remoteTerminal) Key(k ports.KeyEvent) error {
	return t.write(ports.TerminalMessage{Type: "key", Key: &k})
}

func (t *remoteTerminal) Paste(text string) error {
	return t.write(ports.TerminalMessage{Type: "paste", Text: text})
}

func (t *remoteTerminal) Resize(s ports.TermSize) error {
	return t.write(ports.TerminalMessage{Type: "resize", Size: &s})
}

func (t *remoteTerminal) Title() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.title
}

// Kill stops the session on the server and waits for its exit to arrive.
func (t *remoteTerminal) Kill() error {
	select {
	case <-t.done:
		return nil
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := t.client.post(ctx, "/api/sessions/"+url.PathEscape(t.id)+"/stop", map[string]string{}, nil); err != nil {
		return err
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		t.conn.CloseNow()
		<-t.done
	}
	return nil
}

func (t *remoteTerminal) Done() <-chan struct{} { return t.done }

func (t *remoteTerminal) ExitCode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.code
}
