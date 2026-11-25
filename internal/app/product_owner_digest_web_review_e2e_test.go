//go:build !windows

package app

import (
	"context"
	"strings"
	"testing"

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
