package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// rawKey presses a named key (Enter, ArrowDown, Escape…) as one raw keydown
// and a keyup, with no separate keypress: a dropdown that closes on keydown
// would otherwise see the keypress land on whatever took the focus.
func rawKey(key string, code int64) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		down := input.DispatchKeyEvent(input.KeyRawDown).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(code)
		if err := down.Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(code).Do(ctx)
	})
}

func pressEnter() chromedp.Action     { return rawKey("Enter", 13) }
func pressArrowDown() chromedp.Action { return rawKey("ArrowDown", 40) }

// dropdownShot saves the page as name.png in DROPDOWN_SHOTS, in the given
// color scheme; without DROPDOWN_SHOTS it does nothing.
func dropdownShot(name, scheme string) chromedp.Action {
	dir := os.Getenv("DROPDOWN_SHOTS")
	if dir == "" {
		return chromedp.ActionFunc(func(context.Context) error { return nil })
	}
	var buf []byte
	return chromedp.Tasks{
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}, {Name: "prefers-reduced-motion", Value: "reduce"}}),
		chromedp.Sleep(150 * time.Millisecond),
		chromedp.CaptureScreenshot(&buf),
		chromedp.ActionFunc(func(context.Context) error { return os.WriteFile(filepath.Join(dir, name+".png"), buf, 0o644) }),
	}
}

// The new-session form's Agent is a Listbox select: each agent with its
// description, worked from the keyboard, while the hidden native select stays
// the value the form sends. (The form lives in the detail pane, which shows
// from 64rem up, so the phone sheet is covered by the unit suite.)
func TestNewSessionAgentIsAListboxSelect(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	api := &fakeStartAPI{}
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:    fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Sessions: fakeSessions{},
		Agents: fakeAgents{[]*domain.Agent{
			{ID: "a-ux", Name: "ux-designer", Description: "Variants, behaviour, wireframes"},
			{ID: "a-web", Name: "frontend-developer", Description: "Alpine, templ and the design system"},
		}},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness", RootDir: "/src/harness"}}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	trigger := `document.querySelector('#new-session-agent-trigger')`
	var rows []string
	var expanded, chosen, shown, active string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		clickButton(`main header`, "New session"),
		chromedp.Poll(`!!document.querySelector('#new-session-agent-trigger')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`document.querySelector('#new-session-base').value === 'main'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		dropdownShot("new-session-light", "light"),
		chromedp.Evaluate(trigger+`.focus()`, nil),
		pressArrowDown(),
		chromedp.Evaluate(trigger+`.getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(`[...document.querySelectorAll('#new-session-agent-listbox [role=option]')].map(o => o.querySelector('.label').textContent + ' / ' + (o.querySelector('.description')?.textContent ?? ''))`, &rows),
		pressArrowDown(),
		pressArrowDown(),
		chromedp.Evaluate(`document.getElementById(`+trigger+`.getAttribute('aria-activedescendant')).querySelector('.label').textContent`, &active),
		dropdownShot("agent-open-light", "light"),
		dropdownShot("agent-open-dark", "dark"),
		pressEnter(),
		chromedp.Evaluate(`document.querySelector('#new-session-agent').value`, &chosen),
		chromedp.Evaluate(trigger+`.querySelector('.value').textContent`, &shown),
		clickButton(`main form`, "Start session"),
		chromedp.Poll(`!document.querySelector('form[x-data^=sessionsNewSession]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if expanded != "true" {
		t.Errorf("↓ on the trigger: aria-expanded = %q, want true", expanded)
	}
	want := "Plain claude / claude with no agent definition,ux-designer / Variants, behaviour, wireframes,frontend-developer / Alpine, templ and the design system"
	if got := strings.Join(rows, ","); got != want {
		t.Errorf("options = %s\nwant      %s", got, want)
	}
	if active != "frontend-developer" {
		t.Errorf("active after ↓↓ = %q, want frontend-developer", active)
	}
	if chosen != "a-web" || shown != "frontend-developer" {
		t.Errorf("after Enter the select is %q and the trigger shows %q, want a-web / frontend-developer", chosen, shown)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.started["agent_id"] != "a-web" {
		t.Errorf("start request agent = %v, want a-web", api.started["agent_id"])
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
