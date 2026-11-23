package web

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/rfbatista/gohydrate"
)

// The top bar is a React island rendered on the server (ADR 0100): painted
// before any script runs, drawn by the browser when the render fails.

const (
	// barLinks lists the pages the top bar links, by accessible name and href.
	barLinks = `[...document.querySelectorAll('.topbar a[href]')].map(a => (a.getAttribute('aria-label') || a.textContent.trim()) + '>' + a.getAttribute('href')).join(' ')`
	// barPicker is the picker's selected project.
	barPicker = `document.querySelector('.topbar #project-picker')?.value`
)

func TestTheTopBarIsPaintedByTheServerBeforeAnyScriptRuns(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	var links, picker, rendered, label string
	var current, hasMountContent bool
	h.run(t, "without script",
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/projects/p1"),
		chromedp.Evaluate(barLinks, &links),
		chromedp.Evaluate(barPicker, &picker),
		chromedp.Evaluate(`document.querySelector('.topbar [data-island="top-bar"]').getAttribute('data-island-rendered') ?? ''`, &rendered),
		chromedp.Evaluate(`document.querySelector('.topbar [data-island="top-bar"]').hasChildNodes()`, &hasMountContent),
		chromedp.Evaluate(`!!document.querySelector('.topbar a[aria-current="page"]')`, &current),
		chromedp.AttributeValue(`.topbar .bell`, "aria-label", &label, nil, chromedp.ByQuery),
		emulation.SetScriptExecutionDisabled(false),
	)
	if !hasMountContent || rendered != "" {
		t.Errorf("the server must render the mount: content %v, data-island-rendered %q", hasMountContent, rendered)
	}
	if !strings.Contains(links, "harness>/ ") || !strings.Contains(links, "Agents>/agents") || !strings.Contains(links, "Notifications, 3 unread>/notifications") {
		t.Errorf("links without script: %s", links)
	}
	if picker != "p1" || label != "Notifications, 3 unread" || current {
		t.Errorf("picker %q, bell %q, a current link on a project page: %v", picker, label, current)
	}
	h.noErrors(t)
}

func TestTheCurrentPageIsMarkedOnTheServerRender(t *testing.T) {
	h := newInboxHarness(t, inboxWorld())
	var name string
	h.run(t, "agents page without script",
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/agents"),
		chromedp.Evaluate(`document.querySelector('.topbar a[aria-current="page"]')?.textContent.trim()`, &name),
		emulation.SetScriptExecutionDisabled(false),
	)
	if name != "Agents" {
		t.Errorf("current link %q, want Agents", name)
	}
}

// failingRenderer is a renderer forced to fail.
type failingRenderer struct{}

func (failingRenderer) Render(context.Context, string, []byte) (string, error) {
	return "", &gohydrate.RenderError{Island: "top-bar", Kind: gohydrate.ErrRenderFailed, Stack: "at TopBar"}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestAFailedServerRenderStillLoadsTheTopBarAndTheBrowserDrawsIt(t *testing.T) {
	logs := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	h := newInboxHarnessWith(t, inboxWorld(), func(d *Deps) { d.IslandRenderer = failingRenderer{} })
	var rendered string
	var serverDrew bool
	h.run(t, "server HTML",
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/projects/p1"),
		chromedp.Evaluate(`document.querySelector('.topbar [data-island="top-bar"]').getAttribute('data-island-rendered') ?? ''`, &rendered),
		chromedp.Evaluate(`document.querySelector('.topbar [data-island="top-bar"]').hasChildNodes()`, &serverDrew),
		emulation.SetScriptExecutionDisabled(false),
	)
	if rendered != "false" || serverDrew {
		t.Errorf("a failed render leaves an empty marked mount: data-island-rendered %q, content %v", rendered, serverDrew)
	}
	var links, badge string
	h.run(t, "client render",
		h.openReady("/projects/p1"),
		chromedp.Evaluate(barLinks, &links),
		chromedp.Evaluate(bellBadge, &badge),
	)
	if !strings.Contains(links, "harness>/ ") || !strings.Contains(links, "Agents>/agents") || badge != "3" {
		t.Errorf("the browser must draw the bar: links %q, badge %q", links, badge)
	}
	if out := logs.String(); !strings.Contains(out, "kind=render_failed") {
		t.Errorf("the log must name the error kind:\n%s", out)
	}
	h.noErrors(t)
}
