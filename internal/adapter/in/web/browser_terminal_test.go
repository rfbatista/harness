package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/coder/websocket"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakePTY speaks the terminal attach protocol (httpapi/terminal.go): a
// snapshot first, then it echoes typed text back as output and records what
// the browser sent. exit ends the process.
type fakePTY struct {
	mu       sync.Mutex
	received []ports.TerminalMessage
	exit     chan int
}

func (p *fakePTY) messages() []ports.TerminalMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ports.TerminalMessage(nil), p.received...)
}

func (p *fakePTY) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()
	write := func(m ports.TerminalMessage) {
		b, _ := json.Marshal(m)
		_ = conn.Write(ctx, websocket.MessageText, b)
	}
	write(ports.TerminalMessage{Type: "snapshot", Snapshot: &ports.TerminalSnapshot{
		Screen: "hello from the pty\n\x1b[1m$\x1b[0m ", CursorX: 2, CursorY: 1,
		Size: ports.TermSize{Cols: 80, Rows: 24}, Title: "claude",
	}})

	go func() {
		select {
		case code := <-p.exit:
			write(ports.TerminalMessage{Type: "exit", Code: code})
			_ = conn.Close(websocket.StatusNormalClosure, "exited")
		case <-ctx.Done():
		}
	}()
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
		p.mu.Lock()
		p.received = append(p.received, m)
		p.mu.Unlock()
		if m.Type == "key" && m.Key != nil && m.Key.Text != "" {
			_ = conn.Write(ctx, websocket.MessageBinary, []byte(m.Key.Text))
		}
		if m.Type == "key" && m.Key != nil && m.Key.Code == '\r' {
			_ = conn.Write(ctx, websocket.MessageBinary, []byte("\r\nran it\r\n"))
		}
	}
}

func TestTerminalInTheBrowserTalksToThePTY(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	pty := &fakePTY{exit: make(chan int, 1)}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions: fakeSessions{[]*domain.Session{{
			ID: "s1", ProjectID: "p1", TicketID: "t1", Task: "implement this task",
			Status: domain.SessionIdle, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: time.Now(),
		}}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/terminal", pty)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rows := `document.querySelector('.terminal .xterm-rows')?.textContent ?? ''`
	var screen, afterTyping, afterEnter, state, bar string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(rows+`.includes('hello from the pty')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(rows, &screen),
		// Real key events, through xterm's textarea, as a person types.
		chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery),
		chromedp.KeyEvent("ls"),
		chromedp.Poll(rows+`.includes('$ ls')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows, &afterTyping),
		chromedp.KeyEvent("\r"),
		chromedp.Poll(rows+`.includes('ran it')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows, &afterEnter),
		chromedp.Evaluate(`document.querySelector('.terminal .bar .status')?.textContent ?? ''`, &state),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if state != "live" {
		t.Errorf("terminal state = %q, want live", state)
	}

	var keys []string
	resized := false
	for _, m := range pty.messages() {
		switch {
		case m.Type == "key" && m.Key != nil:
			keys = append(keys, strings.ReplaceAll(string(m.Key.Code), "\r", `\r`))
		case m.Type == "resize" && m.Size != nil && m.Size.Cols > 0:
			resized = true
		}
	}
	if strings.Join(keys, "") != `ls\r` {
		t.Errorf("keys the PTY received = %q, want l, s, enter", keys)
	}
	if !resized {
		t.Error("the browser never sent its terminal size")
	}

	pty.exit <- 0
	err = chromedp.Run(ctx,
		chromedp.Poll(`document.querySelector('.terminal .bar .status')?.textContent === 'exited (code 0)'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.terminal .bar')?.textContent ?? ''`, &bar),
	)
	if err != nil {
		t.Fatalf("exit not shown: %v (bar %q)", err, bar)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
