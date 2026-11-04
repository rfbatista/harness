//go:build !windows

package web_test

import (
	"strings"
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	attrPreview = `document.querySelector('#attribution-preview')?.textContent.trim()`
	// attrSet sets a control of #attribution the way a person does.
	attrSet  = `((id, value) => { const e = document.getElementById(id); e.value = value; e.dispatchEvent(new Event(e.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true })); })`
	attrSave = `[...document.querySelectorAll('#attribution button')].find(b => b.textContent.trim() === 'Save').click()`
)

// The Agent attribution section of the Agent isolation page, end to end: the
// settings in SQLite, the API and the page, and a person in headless Chrome.
func TestAgentAttributionIsSetOnTheAgentIsolationPage_EndToEnd(t *testing.T) {
	srv, _ := agentIsolationServer(t, nil)
	b := newBrowser(t)

	b.run("the section is on the page and starts on Claude default",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`document.getElementById('attribution')?.tagName === 'SECTION'`),
		waitUntil(`document.querySelector('#attribution-commit-mode')?.value === 'default' && document.querySelector('#attribution-pr-mode')?.value === 'default'`),
		waitUntil(`!document.querySelector('#attribution [data-ssr]')`),
		waitUntil(attrPreview+` === '{}'`),
	)
	b.run("the preview follows the controls before any save",
		chromedp.Evaluate(attrSet+`('attribution-commit-mode', 'none')`, nil),
		waitUntil(attrPreview+` === '{"attribution":{"commit":""}}'`),
		chromedp.Evaluate(attrSet+`('attribution-pr-mode', 'custom')`, nil),
		waitUntil(`document.querySelector('#attribution-pr-text')?.checkVisibility() === true`),
		chromedp.Evaluate(attrSet+`('attribution-pr-text', '  Made with care  ')`, nil),
		waitUntil(attrPreview+` === '{"attribution":{"commit":"","pr":"Made with care"}}'`),
	)
	b.run("saving keeps the values across a reload",
		chromedp.Evaluate(attrSave, nil),
		waitUntil(`document.querySelector('#attribution').innerText.includes('Saved.')`),
		chromedp.Reload(),
		waitUntil(`document.querySelector('#attribution-commit-mode')?.value === 'none' && document.querySelector('#attribution-pr-mode')?.value === 'custom'`),
		waitUntil(`document.querySelector('#attribution-pr-text')?.value === 'Made with care'`),
		waitUntil(attrPreview+` === '{"attribution":{"commit":"","pr":"Made with care"}}'`),
	)
	var page, served string
	b.run("the preview equals the JSON the server answers",
		chromedp.Evaluate(attrPreview, &page),
		chromedp.Evaluate(`fetch('/api/get_agent_attribution').then(r => r.json()).then(j => j.preview)`, &served, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	)
	if page != served || served != `{"attribution":{"commit":"","pr":"Made with care"}}` {
		t.Errorf("page preview %q, server preview %q", page, served)
	}

	b.run("empty custom text is refused with its message and the saved values stay",
		chromedp.Evaluate(attrSet+`('attribution-commit-mode', 'custom')`, nil),
		chromedp.Evaluate(attrSet+`('attribution-commit-text', '   ')`, nil),
		chromedp.Evaluate(attrSave, nil),
		waitUntil(`document.querySelector('#attribution-error')?.textContent.includes('Commits: enter the custom text')`),
		waitUntil(`document.querySelector('#attribution-commit-mode').value === 'none'`),
		waitUntil(attrPreview+` === '{"attribution":{"commit":"","pr":"Made with care"}}'`),
	)
	b.run("the saved values are still there after a reload",
		chromedp.Reload(),
		waitUntil(`document.querySelector('#attribution-commit-mode')?.value === 'none'`),
	)
	b.run("back to Claude default leaves nothing to pass",
		chromedp.Evaluate(attrSet+`('attribution-commit-mode', 'default')`, nil),
		chromedp.Evaluate(attrSet+`('attribution-pr-mode', 'default')`, nil),
		waitUntil(attrPreview+` === '{}'`),
		chromedp.Evaluate(attrSave, nil),
		waitUntil(`document.querySelector('#attribution').innerText.includes('Saved.')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
