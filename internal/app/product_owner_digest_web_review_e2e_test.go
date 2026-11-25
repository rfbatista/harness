//go:build !windows

package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Review tests for the digest time in the browser: gaps found in review of
// agent/project-product-owner-60e33acf.

// recordDigestTimeWrites makes the page keep, in window.__digestTimes, every
// digest_time the web UI sends to the server.
func recordDigestTimeWrites() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(`(() => {
			window.__digestTimes = [];
			const send = window.fetch;
			window.fetch = (input, init) => {
				if (String(input).includes('/api/update_product_owner') && typeof init?.body === 'string') {
					const body = JSON.parse(init.body);
					if ('digest_time' in body) window.__digestTimes.push(body.digest_time);
				}
				return send(input, init);
			};
		})()`).Do(ctx)
		return err
	})
}

// A person types a new time over the one that is set, with the keyboard, as
// Chrome's native time input takes it: one segment at a time. Each keystroke
// makes the input's value a complete but unintended time ("01:00" on the way
// to "10:45"). Saving those would let the scheduler, which ticks once a
// minute, treat a passed time as due and record a digest the person never
// asked for, so only the time the person ends on may reach the server.
func TestTypingATimeOverTheDigestTimeSavesOnlyTheTimeTypedInTheWebUI_EndToEnd(t *testing.T) {
	w, _ := newPODigestWeb(t)
	w.setDigestTime("09:00")
	p := newPerson(t)
	project := w.url + "/projects/" + w.project

	p.run(t, "the person opens the Settings page with 09:00 set",
		recordDigestTimeWrites(),
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(project+"/settings"),
		waitFor(`!!`+digestInput+` && !`+digestInput+`.disabled && `+digestInput+`.value === '09:00'`),
	)
	var box []float64
	p.run(t, "read the input's box",
		chromedp.ScrollIntoView(`#digest-time`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const r = `+digestInput+`.getBoundingClientRect(); return [r.left, r.top, r.height]; })()`, &box),
	)
	p.run(t, "the person clicks the hours and types 10:45",
		chromedp.MouseClickXY(box[0]+10, box[1]+box[2]/2),
		chromedp.KeyEvent("1045"),
		waitFor(digestInput+`.value === '10:45'`),
		// Enter ends the typing: only then is the time saved.
		pressEnter(),
		waitFor(`window.__digestTimes.includes('10:45')`),
	)
	if got := w.settings().DigestTime; got == nil || *got != "10:45" {
		t.Errorf("the server holds the digest time %v, want 10:45", got)
	}
	if got := p.text(t, `window.__digestTimes.join(',')`); got != "10:45" {
		t.Errorf("the web UI sent the digest times %s, want only 10:45: each one is a time the scheduler may act on", strings.ReplaceAll(got, ",", ", "))
	}
}

// A person types a time and then clicks Clear time, without leaving the
// input first. The mousedown blurs the input, and the blur saves the typed
// time. While that save is on its way the button is disabled, so the click
// that follows is lost. The person asked for no time, and the server ends up
// holding the one they typed.
func TestClearTimeRightAfterTypingClearsTheDigestTimeInTheWebUI_EndToEnd(t *testing.T) {
	w, _ := newPODigestWeb(t)
	w.setDigestTime("09:00")
	p := newPerson(t)
	project := w.url + "/projects/" + w.project

	p.run(t, "the person opens the Settings page with 09:00 set",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(project+"/settings"),
		waitFor(`!!`+digestInput+` && !`+digestInput+`.disabled && `+digestInput+`.value === '09:00'`),
	)
	var box []float64
	p.run(t, "read the input's box",
		chromedp.ScrollIntoView(`#digest-time`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const r = `+digestInput+`.getBoundingClientRect(); return [r.left, r.top, r.height]; })()`, &box),
	)
	clear := `[...document.querySelectorAll('#product-owner .digest-time button')].find(b => b.textContent.trim() === 'Clear time')`
	var at []float64
	p.run(t, "the person types 10:45, then clicks Clear time with the mouse",
		chromedp.MouseClickXY(box[0]+10, box[1]+box[2]/2),
		chromedp.KeyEvent("1045"),
		waitFor(digestInput+`.value === '10:45'`),
		chromedp.Evaluate(`(() => { const r = `+clear+`.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`, &at),
	)
	p.run(t, "Clear time is clicked",
		chromedp.MouseClickXY(at[0], at[1]),
		waitFor(`/no digest is recorded until a time is set|next: /.test(`+digestNote+`) && !`+clear+`?.disabled`),
	)
	// The blur's save and the clear answer within this time on the loopback server.
	p.run(t, "the answers settle",
		chromedp.Sleep(1500*time.Millisecond),
		waitFor(`!document.querySelector('#digest-time[aria-busy=true]')`),
	)
	if got := w.settings().DigestTime; got != nil {
		t.Errorf("the server holds the digest time %s after Clear time, want none", *got)
	}
}

// The page's three tabs and its address, after the merge of the Memory tab.
// The Memory tab chosen from the Digests tab must not keep "#digests" in the
// address: a reload would open the Digests tab again. An address that names
// the Memory tab while the Digests tab is open must open the Memory tab, not
// the Threads tab.
func TestTheMemoryTabAndTheDigestsTabKeepTheAddressInStepInTheWebUI_EndToEnd(t *testing.T) {
	w, _ := newPODigestWeb(t)
	p := newPerson(t)
	page := w.url + "/projects/" + w.project + "/product-owner"
	selected := `(document.querySelector('[role=tab][aria-selected=true]')?.textContent ?? '')`
	tab := func(name string) string {
		return `[...document.querySelectorAll('[role=tab]')].find(t => t.textContent.trim().startsWith('` + name + `'))`
	}
	var at []float64

	p.run(t, "the person opens the Digests tab and reads the Memory tab's place",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(page+"#digests"),
		waitFor(selected+`.trim() === 'Digests'`),
		chromedp.Evaluate(`(() => { const r = `+tab("Memory")+`.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2]; })()`, &at),
	)
	p.run(t, "a click on the Memory tab opens it",
		chromedp.MouseClickXY(at[0], at[1]),
		waitFor(selected+`.trim().startsWith('Memory')`),
	)
	if got := p.text(t, `location.hash`); strings.HasPrefix(got, "#digests") {
		t.Errorf("the Memory tab is open and the address still names the Digests tab (%s): a reload opens the Digests tab", got)
	}

	p.run(t, "back on the Digests tab, the address comes to name the Memory tab",
		chromedp.Navigate("about:blank"),
		chromedp.Navigate(page+"#digests"),
		waitFor(selected+`.trim() === 'Digests'`),
		chromedp.Evaluate(`location.hash = '#memory'`, nil),
		chromedp.Sleep(500*time.Millisecond),
	)
	if got := p.text(t, selected); !strings.HasPrefix(strings.TrimSpace(got), "Memory") {
		t.Errorf("the address names the Memory tab and the %q tab is open", strings.TrimSpace(got))
	}
}
