package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const changesList = `section[aria-label=Code] .diff-summary`

// TestTheChangesListGroupsFiltersAndCountsTheWorkingScope: files sit under a
// head naming their directory once, the legend counts each change letter and
// filters as a pressed toggle, the Working segment carries its count and the
// Working scope splits Tracked changes from Untracked.
func TestTheChangesListGroupsFiltersAndCountsTheWorkingScope(t *testing.T) {
	w := syncWorld(t, aheadOfItsRemote)
	ctx, errs := browser(t)

	var groups, legend, working, afterPress, afterRelease, head, note []string
	var pressedBefore, pressedAfter []string
	var rowDirs int
	var headText, headAfterPress string
	var sections, sectionNames []string
	var workingHead string
	var shot, workingShot []byte
	rowNames := func(into *[]string) chromedp.Action {
		return chromedp.Evaluate(`[...document.querySelectorAll('`+changesList+` .changed a .name')].map(n => n.textContent)`, into)
	}
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(codeURL(w)),
		pollFor(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('`+changesList+` .changed a').length > 0`),
		chromedp.Evaluate(`[...document.querySelectorAll('`+changesList+` li.group .group-head')].map(h => h.innerText.replace(/\s+/g, ' ').trim())`, &groups),
		chromedp.Evaluate(`document.querySelectorAll('`+changesList+` .changed a .dir').length`, &rowDirs),
		chromedp.Evaluate(`[...document.querySelectorAll('`+changesList+` .legend button')].map(b => b.innerText.replace(/\s+/g, ' ').trim())`, &legend),
		chromedp.Evaluate(`[...document.querySelectorAll('`+changesList+` .legend button')].map(b => b.getAttribute('aria-pressed'))`, &pressedBefore),
		chromedp.Evaluate(`document.querySelector('`+changesList+` .head .totals').innerText.replace(/\s+/g, ' ').trim()`, &headText),
		chromedp.Evaluate(`[...document.querySelectorAll('`+changesList+` .head .segmented a')].map(a => a.innerText.replace(/\s+/g, ' ').trim())`, &working),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),
		// press the first legend letter: only its files stay, the totals do not move, the head says what is shown
		chromedp.Evaluate(`document.querySelector('`+changesList+` .legend button').click()`, nil),
		pollFor(`document.querySelector('`+changesList+` .legend button').getAttribute('aria-pressed') === 'true'`),
		rowNames(&afterPress),
		chromedp.Evaluate(`[...document.querySelectorAll('`+changesList+` .legend button')].map(b => b.getAttribute('aria-pressed'))`, &pressedAfter),
		chromedp.Evaluate(`document.querySelector('`+changesList+` .head .totals').innerText.replace(/\s+/g, ' ').trim()`, &headAfterPress),
		chromedp.Evaluate(`document.querySelector('`+changesList+` .legend button').click()`, nil),
		pollFor(`document.querySelector('`+changesList+` .legend button').getAttribute('aria-pressed') === 'false'`),
		rowNames(&afterRelease),
	)
	if err != nil {
		var dump string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('`+changesList+`')?.outerHTML.slice(0, 2500) ?? 'no list'`, &dump))
		t.Fatalf("%v\n%s\nJS errors: %v", err, dump, errs.all())
	}
	saveShot("code-changes-grouped.jpg", shot)
	_, _, _, _ = head, note, sections, sectionNames
	if len(groups) < 3 || !containsPrefix(groups, "internal/app/") || !containsPrefix(groups, "web/src/") {
		t.Errorf("group heads = %v", groups)
	}
	if rowDirs != 0 {
		t.Errorf("%d rows still carry a directory: the group head names it once", rowDirs)
	}
	if len(legend) == 0 || !strings.HasPrefix(legend[0], "M ") {
		t.Errorf("legend = %v, want M first with its count", legend)
	}
	for _, p := range pressedBefore {
		if p != "false" {
			t.Errorf("a legend toggle starts pressed: %v", pressedBefore)
		}
	}
	if len(afterPress) == 0 || len(afterPress) >= len(afterRelease) {
		t.Errorf("pressing M left %v of %v", afterPress, afterRelease)
	}
	if pressedAfter[0] != "true" {
		t.Errorf("pressed = %v", pressedAfter)
	}
	if !strings.HasPrefix(headAfterPress, headText) || !strings.Contains(headAfterPress, "· showing modified") {
		t.Errorf("head %q → %q, want the whole totals then '· showing modified'", headText, headAfterPress)
	}
	joined := strings.Join(working, "|")
	if !strings.Contains(joined, "Working 3") {
		t.Errorf("segments = %q, want Working to carry its count (3)", joined)
	}

	// The Working scope: Tracked changes and Untracked, each with its count; the head names the new files.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('`+changesList+` .head .segmented a')].find(a => a.textContent.trim().startsWith('Working')).click()`, nil),
		pollFor(`document.querySelectorAll('`+changesList+` li.section').length === 2`),
		chromedp.Evaluate(`[...document.querySelectorAll('`+changesList+` li.section > .title')].map(h => h.innerText.replace(/\s+/g, ' ').trim())`, &sections),
		chromedp.Evaluate(`document.querySelector('`+changesList+` .head .totals').innerText.replace(/\s+/g, ' ').trim()`, &workingHead),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.FullScreenshot(&workingShot, 85),
	)
	if err != nil {
		t.Fatalf("working: %v\nJS errors: %v", err, errs.all())
	}
	saveShot("code-changes-working.jpg", workingShot)
	if len(sections) != 2 || !strings.HasPrefix(sections[0], "Tracked changes vs HEAD") || !strings.HasPrefix(sections[1], "Untracked") {
		t.Errorf("sections = %v", sections)
	}
	if !strings.Contains(workingHead, "new file") {
		t.Errorf("working head = %q, want the new files named", workingHead)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

func containsPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
