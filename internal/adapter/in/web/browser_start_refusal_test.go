package web

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeAgentCredential struct{ set atomic.Bool }

func (f *fakeAgentCredential) Status() ports.AgentCredentialStatus {
	return ports.AgentCredentialStatus{Set: f.set.Load()}
}
func (f *fakeAgentCredential) Set(string, string) (ports.AgentCredentialStatus, error) {
	return f.Status(), nil
}
func (f *fakeAgentCredential) Remove() (ports.AgentCredentialStatus, error) { return f.Status(), nil }

type fakeClaudeBin struct{ installed atomic.Bool }

func (f *fakeClaudeBin) Bin() string      { return "/data/claude/current" }
func (f *fakeClaudeBin) Installed() bool  { return f.installed.Load() }
func (f *fakeClaudeBin) Overridden() bool { return false }

type fakeSandboxSwitch struct{}

func (fakeSandboxSwitch) Enabled() bool              { return false }
func (fakeSandboxSwitch) SetEnabled(bool) error      { return nil }
func (fakeSandboxSwitch) Paths() []ports.SandboxPath { return nil }
func (fakeSandboxSwitch) AddPath(ports.SandboxPath) ([]ports.SandboxPath, error) {
	return nil, nil
}
func (fakeSandboxSwitch) SetPathAccess(string, bool) ([]ports.SandboxPath, error) { return nil, nil }
func (fakeSandboxSwitch) RemovePath(string) ([]ports.SandboxPath, error)          { return nil, nil }

// refusingStartAPI answers a start the way the harness refuses one with no
// credential and no managed claude yet: a coded error with what is missing.
type refusingStartAPI struct{ cred *fakeAgentCredential }

func (f refusingStartAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/list_branches":
		_ = json.NewEncoder(w).Encode(map[string]any{"branches": []map[string]any{{"name": "main", "is_head": true}}})
	case "/api/start_interactive_session":
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "Agent not started: no agent credential. Add one in Agent isolation settings (/agent-isolation#credential). Agent not started: the managed claude is installing (50%). Try again when it is done (/agent-isolation#claude).",
			"code":  "AGENT_CREDENTIAL_MISSING",
			"details": map[string]any{"missing": []map[string]string{
				{"code": "AGENT_CREDENTIAL_MISSING", "anchor": "/agent-isolation#credential"},
				{"code": "MANAGED_CLAUDE_INSTALLING", "anchor": "/agent-isolation#claude"},
			}},
		})
	default:
		http.NotFound(w, r)
	}
}

// A refused start shows in the form with one working link per missing thing,
// and the Agent isolation entry in the top bar carries the amber mark while
// the credential or the managed claude is missing.
func TestARefusedStartShowsItsLinksAndTheTopBarMarkFollowsWhatIsMissing(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	cred := &fakeAgentCredential{}
	bin := &fakeClaudeBin{}
	pages := NewHandler(Deps{
		Projects:         fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:            fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Sessions:         fakeSessions{},
		Agents:           fakeAgents{},
		Repositories:     fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness", RootDir: "/src/harness"}}},
		SandboxSettings:  fakeSandboxSwitch{},
		AgentCredential:  cred,
		ManagedClaudeBin: bin,
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", refusingStartAPI{cred})
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	poll := func(js string) chromedp.Action {
		return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	const mark = `document.querySelector('header a[aria-label="Agent isolation"]')?.hasAttribute('data-blocking')`
	const links = `[...document.querySelectorAll('form[x-data^=sessionsNewSession] a[data-refusal-link]')].map(a => a.textContent.trim() + ' -> ' + a.getAttribute('href')).join(' | ')`

	var shown string
	var pathAfter string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		poll(mark+` === true`), // nothing is set up yet
		clickButton(`main header`, "New session"),
		poll(`document.querySelectorAll('#new-session-base option').length === 2`),
		clickButton(`main form`, "Start session"),
		poll(links+` !== ''`),
		chromedp.Evaluate(links, &shown),
		// The link works: it lands on the credential section of the page.
		chromedp.Click(`form[x-data^=sessionsNewSession] a[data-refusal-link]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#credential`, chromedp.ByID),
		chromedp.Evaluate(`location.pathname + location.hash`, &pathAfter),
		poll(mark+` === true`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if pathAfter != "/agent-isolation#credential" {
		t.Errorf("the link led to %q", pathAfter)
	}
	want := "Add one in Agent isolation settings -> /agent-isolation#credential | Follow the install in Agent isolation settings -> /agent-isolation#claude"
	if shown != want {
		t.Errorf("links = %q\nwant    %q", shown, want)
	}

	// The credential is added: the mark stays while the claude is missing, and goes once both are met.
	cred.set.Store(true)
	err = chromedp.Run(ctx, chromedp.Reload(), poll(`!!document.querySelector('header a[aria-label="Agent isolation"]')`), poll(mark+` === true`))
	if err != nil {
		t.Fatalf("with the credential only: %v", err)
	}
	bin.installed.Store(true)
	err = chromedp.Run(ctx, chromedp.Reload(), poll(`!!document.querySelector('header a[aria-label="Agent isolation"]')`), poll(mark+` === false`))
	if err != nil {
		t.Fatalf("with both: %v", err)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%v", e)
	}
}
