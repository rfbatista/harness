//go:build !windows

package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/runtimetest"
)

// fakePersons is the person's terminals reduced to what the routes need: one
// terminal "pt_1" on a real host, and a refusal to give when asked.
type fakePersons struct {
	host    *termhost.Host
	opened  ports.PersonTerminalOpen
	session string
	refuse  error
}

func (f *fakePersons) Open(_ context.Context, sessionID string, req ports.PersonTerminalOpen) (ports.PersonTerminal, error) {
	f.session, f.opened = sessionID, req
	if f.refuse != nil {
		return ports.PersonTerminal{}, f.refuse
	}
	return ports.PersonTerminal{ID: "pt_1", SessionID: sessionID, Root: req.Root, Dir: req.Dir, Label: req.Dir, State: ports.PersonTerminalRunning, OpenedAt: time.Unix(1, 0).UTC()}, nil
}

func (f *fakePersons) List(_ context.Context, sessionID string) ([]ports.PersonTerminal, error) {
	return []ports.PersonTerminal{{ID: "pt_1", SessionID: sessionID, Root: "worktree", State: ports.PersonTerminalRunning}}, nil
}

func (f *fakePersons) Attach(id string) (ports.Terminal, error) { return f.host.Attach(id) }
func (f *fakePersons) Close(string) error                       { return nil }

func personRouter(t *testing.T, opts ...Option) (*httptest.Server, *fakePersons) {
	t.Helper()
	host := termhost.New(shell.Direct{}, ptyunix.New(), runtimetest.Script{})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	if err := host.Spawn(context.Background(), "pt_1", runtimetest.Spec("pt_1", t.TempDir(), "sleep 30"), ports.TermSize{Cols: 80, Rows: 24}, nil); err != nil {
		t.Fatal(err)
	}
	f := &fakePersons{host: host}
	srv := httptest.NewServer(NewRouter(NewHandler(Services{PersonTerminals: f}), opts...))
	t.Cleanup(srv.Close)
	return srv, f
}

// personRoutes are every route of a person's terminal, the WebSocket upgrade
// among them.
var personRoutes = map[string]struct{ method, path, body string }{
	"open":   {"POST", "/api/sessions/s1/person-terminals", `{"root":"worktree","dir":"","cols":80,"rows":24}`},
	"list":   {"GET", "/api/sessions/s1/person-terminals", ""},
	"attach": {"GET", "/api/person-terminals/pt_1/terminal", ""},
	"close":  {"DELETE", "/api/person-terminals/pt_1", ""},
}

// personCall makes the request with host and origin as given ("" leaves the header
// out) and answers the status and the code of the refusal.
func personCall(t *testing.T, srv *httptest.Server, r struct{ method, path, body string }, host, origin, auth string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(r.method, srv.URL+r.path, strings.NewReader(r.body))
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if r.path[len(r.path)-8:] == "terminal" {
		// An upgrade request, as a browser makes it.
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &out)
	return resp.StatusCode, out.Code
}

func TestPersonTerminalGuardRefusesEveryRouteOnAnExposedServerWithoutAToken(t *testing.T) {
	srv, _ := personRouter(t, WithExposedListener(true))
	for name, r := range personRoutes {
		t.Run(name, func(t *testing.T) {
			if status, code := personCall(t, srv, r, "", "", ""); status != 403 || code != "TERMINAL_SERVER_EXPOSED" {
				t.Errorf("= %d %q, want 403 TERMINAL_SERVER_EXPOSED", status, code)
			}
		})
	}
}

func TestPersonTerminalGuardPassesAnExposedServerThatHasATokenButStillWantsIt(t *testing.T) {
	srv, _ := personRouter(t, WithExposedListener(true), WithToken("s3cret"))
	if status, code := personCall(t, srv, personRoutes["list"], "", "", ""); status != 401 || code != "UNAUTHORIZED" {
		t.Errorf("without the token = %d %q, want 401 UNAUTHORIZED", status, code)
	}
	if status, _ := personCall(t, srv, personRoutes["list"], "", "", "Bearer s3cret"); status != 200 {
		t.Errorf("with the token = %d, want 200", status)
	}
}

func TestPersonTerminalGuardRefusesAHostThatIsNotLoopbackOnEveryRoute(t *testing.T) {
	srv, _ := personRouter(t)
	for name, r := range personRoutes {
		for _, host := range []string{"evil.example", "evil.example:8080", "10.0.0.5:8080", "localhost.evil.example"} {
			t.Run(name+" "+host, func(t *testing.T) {
				if status, code := personCall(t, srv, r, host, "", ""); status != 403 || code != "TERMINAL_HOST_REFUSED" {
					t.Errorf("= %d %q, want 403 TERMINAL_HOST_REFUSED", status, code)
				}
			})
		}
	}
}

func TestPersonTerminalGuardAcceptsEveryLoopbackHost(t *testing.T) {
	srv, _ := personRouter(t)
	for _, host := range []string{"localhost", "localhost:8080", "127.0.0.1", "127.0.0.1:8080", "[::1]", "[::1]:8080"} {
		if status, code := personCall(t, srv, personRoutes["list"], host, "", ""); status != 200 {
			t.Errorf("Host %q = %d %q, want 200", host, status, code)
		}
	}
}

func TestPersonTerminalGuardRefusesAForeignOriginOnThePostAndOnTheWebSocketUpgrade(t *testing.T) {
	srv, _ := personRouter(t)
	for name, r := range personRoutes {
		for _, origin := range []string{"https://evil.example", "http://127.0.0.1:1", "null"} {
			t.Run(name+" "+origin, func(t *testing.T) {
				if status, code := personCall(t, srv, r, "", origin, ""); status != 403 || code != "TERMINAL_ORIGIN_REFUSED" {
					t.Errorf("= %d %q, want 403 TERMINAL_ORIGIN_REFUSED", status, code)
				}
			})
		}
	}
}

func TestPersonTerminalGuardPassesTheHarnessOwnOriginAndNoOrigin(t *testing.T) {
	srv, _ := personRouter(t)
	own := srv.URL // the Host the request carries is this one
	if status, code := personCall(t, srv, personRoutes["open"], "", "", ""); status != 201 {
		t.Errorf("a loopback request with no Origin = %d %q, want 201", status, code)
	}
	if status, code := personCall(t, srv, personRoutes["open"], "", own, ""); status != 201 {
		t.Errorf("the harness's own origin = %d %q, want 201", status, code)
	}
}

func TestPersonTerminalWebSocketUpgradeIsGuardedBeforeItUpgrades(t *testing.T) {
	srv, _ := personRouter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/person-terminals/pt_1/terminal"

	_, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	if err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("a foreign origin dialed in: %v, %v", resp, err)
	}

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("a browserless client on loopback was refused: %v", err)
	}
	defer conn.CloseNow()
	_, data, err := conn.Read(ctx)
	var m ports.TerminalMessage
	if err != nil || json.Unmarshal(data, &m) != nil || m.Type != "snapshot" {
		t.Errorf("first frame = %s (%v), want a snapshot", data, err)
	}
}

func TestPersonTerminalAttachToAnUnknownIdIsTerminalNotFoundBeforeTheUpgrade(t *testing.T) {
	srv, _ := personRouter(t)
	r := struct{ method, path, body string }{"GET", "/api/person-terminals/pt_nope/terminal", ""}
	if status, code := personCall(t, srv, r, "", "", ""); status != 404 || code != "TERMINAL_NOT_FOUND" {
		t.Errorf("= %d %q, want 404 TERMINAL_NOT_FOUND", status, code)
	}
}

func TestPersonTerminalRoutesFollowTheContract(t *testing.T) {
	srv, f := personRouter(t)
	post := func(path, body string) (int, map[string]any) {
		t.Helper()
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}

	status, m := post("/api/sessions/s1/person-terminals", `{"root":"worktree","dir":"web/src","cols":120,"rows":32}`)
	if status != 201 || m["id"] != "pt_1" || m["session_id"] != "s1" || m["root"] != "worktree" || m["dir"] != "web/src" || m["label"] != "web/src" || m["state"] != "running" || m["opened_at"] == nil {
		t.Fatalf("open = %d %v", status, m)
	}
	if f.session != "s1" || f.opened.Root != "worktree" || f.opened.Dir != "web/src" || f.opened.Size != (ports.TermSize{Cols: 120, Rows: 32}) {
		t.Errorf("the service was asked %q %+v", f.session, f.opened)
	}

	resp, err := http.Get(srv.URL + "/api/sessions/s1/person-terminals")
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(list) != 1 || list[0]["id"] != "pt_1" {
		t.Errorf("list = %d %v", resp.StatusCode, list)
	}

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/person-terminals/pt_1", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 204 {
		t.Errorf("close = %v, %v, want 204", resp, err)
	}
}

func TestPersonTerminalOpenRefusalsKeepTheirCodesAndStatuses(t *testing.T) {
	srv, f := personRouter(t)
	for code, status := range map[string]int{
		"TERMINAL_READ_ONLY_UNAVAILABLE": 409, "ROOT_GONE": 410, "PATH_NOT_FOUND": 404, "INVALID_INPUT": 400, "SESSION_NOT_FOUND": 404,
	} {
		f.refuse = &domain.StructuredError{Code: code, Message: "no"}
		if got, c := personCall(t, srv, personRoutes["open"], "", "", ""); got != status || c != code {
			t.Errorf("%s = %d %q, want %d", code, got, c, status)
		}
	}
}
