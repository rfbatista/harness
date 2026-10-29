package web

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// The Changes list refreshes by itself on code_changed and git_state, the
// hunks stay where they are until Reload, and a session that is not running
// says so (ADR 0048, spec stories 15 to 17 and 19).

const (
	codeBar    = codeSection + ` .code-source-bar`
	codeRows   = codeSection + ` .diff-summary .changed a`
	codeNotice = codeSection + ` .diff-view > .notice`
)

// agentWrites is what the agent does to the worktree: a line appended to a
// tracked file (its stat moves) and a new file (untracked).
func agentWrites(w *codeWorld, newFile string) error {
	readme := filepath.Join(w.f.Worktree, "README.md")
	b, err := os.ReadFile(readme)
	if err != nil {
		return err
	}
	if err := os.WriteFile(readme, append(b, []byte("a line the agent wrote\nand one more\n")...), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.f.Worktree, newFile), []byte("new\nfile\n"), 0o644)
}

func codeChangedMsg(w *codeWorld) string {
	return `{"code_changed":{"workspace_id":"` + w.at.WorkspaceID + `","head":"` + w.f.SessionHead + `"}}`
}

// marksSections puts a property on every file section: one that is drawn
// again is a new element and has lost it.
const marksSections = `(() => { const s = [...document.querySelectorAll('` + codeSection + ` section.file')]; s.forEach(e => { e.__mark = true }); return s.length })()`
const unmarked = `[...document.querySelectorAll('` + codeSection + ` section.file')].filter(e => !e.__mark).map(e => e.id)`

// placeOf is where the long file's section sits in the scroller: what the reader sees move or stay.
const placeOf = `(() => { const m = document.querySelector('` + codeSection + ` .main'); return Math.round(document.querySelector('` + codeSection + ` section.file[id="diff-arrived-txt"]').getBoundingClientRect().top - m.getBoundingClientRect().top) })()`
const marked = `[...document.querySelectorAll('` + codeSection + ` section.file')].filter(e => e.__mark).length`

// TestTheChangesListFollowsTheAgentAndTheHunksStayUntilReload: an event
// refreshes the list, the totals and the count by itself; no section moves,
// the rows out of date carry a dot and the notice counts them; R reads the
// open files again, drawing only those that differ, keeping scroll; a second R
// says Up to date; a file read again says how long ago.
func TestTheChangesListFollowsTheAgentAndTheHunksStayUntilReload(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID }))
	if err := os.WriteFile(filepath.Join(w.f.Worktree, "arrived.txt"), []byte(strings.Repeat("a line of a long file\n", 400)), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code/changes"
	step := ""
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { step = name; return nil })
	}

	var sections, marks int
	var placeBefore, placeAfter, placeReloaded float64
	var dirtyBefore, dirtyAfter, head, word, notice, dots, updated, dotsLeft string
	var sectionForNew bool
	var strayAfterEvent, strayAfterReload []string
	var markedAfterReload int
	var noticeAfterReload, upToDate, upToDateState string
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		at("loaded"),
		chromedp.Poll(`document.querySelector('`+codeBar+`')?.dataset.state === 'live' && document.querySelectorAll('`+codeRows+`').length === 6 && document.querySelector('`+codeSection+` .diff-view .code-lines .line')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		w.feed.connected(),
		chromedp.Evaluate(marksSections, &sections),
		chromedp.Evaluate(`window.__dirty0 = document.querySelector('`+codeBar+` .dirty')?.textContent ?? ''`, &dirtyBefore),
		chromedp.Evaluate(`(() => { const m = document.querySelector('`+codeSection+` .main'); m.scrollTop = 500; m.dispatchEvent(new Event('scroll')); return m.scrollTop })()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(placeOf, &placeBefore),

		at("event"),
		chromedp.ActionFunc(func(context.Context) error {
			if err := agentWrites(w, "later.txt"); err != nil {
				return err
			}
			w.feed.send(codeChangedMsg(w))
			return nil
		}),
		chromedp.Poll(`document.querySelectorAll('`+codeRows+`').length === 7 && document.querySelector('`+codeBar+`').dataset.state === 'stale'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`!!document.querySelector('`+codeNotice+`') && document.querySelector('`+codeBar+` .dirty')?.textContent !== window.__dirty0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+codeSection+` .diff-summary .totals').textContent.replace(/\s+/g, ' ').trim()`, &head),
		chromedp.Evaluate(`document.querySelector('`+codeBar+` .state').textContent.trim()`, &word),
		chromedp.Evaluate(`document.querySelector('`+codeNotice+`').innerText.replace(/\s+/g, ' ').trim()`, &notice),
		chromedp.Evaluate(`[...document.querySelectorAll('`+codeSection+` .diff-summary .changed li[data-updated] .name')].map(n => n.textContent).sort().join(',')`, &dots),
		chromedp.Evaluate(`document.querySelector('`+codeBar+` .dirty')?.textContent ?? ''`, &dirtyAfter),
		chromedp.Evaluate(unmarked, &strayAfterEvent),
		chromedp.Evaluate(marked, &marks),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(placeOf, &placeAfter),
		chromedp.Evaluate(`!!document.querySelector('`+codeSection+` section.file[id="diff-later-txt"]')`, &sectionForNew),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),

		at("reload"),
		chromedp.Focus(codeSection+` .main`, chromedp.ByQuery),
		chromedp.KeyEvent("r"),
		chromedp.Poll(`!!document.querySelector('`+codeSection+` section.file[id="diff-later-txt"]') && document.querySelector('`+codeBar+`').dataset.state === 'live'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`!!document.querySelector('`+codeSection+` section.file[id="diff-README-md"] .head .updated')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+codeSection+` section.file[id="diff-README-md"] .head .updated').textContent.trim()`, &updated),
		chromedp.Evaluate(unmarked, &strayAfterReload),
		chromedp.Evaluate(marked, &markedAfterReload),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(placeOf, &placeReloaded),
		chromedp.Evaluate(`document.querySelector('`+codeNotice+`')?.innerText ?? ''`, &noticeAfterReload),
		chromedp.Evaluate(`[...document.querySelectorAll('`+codeSection+` .diff-summary .changed li[data-updated] .name')].map(n => n.textContent).join(',')`, &dotsLeft),

		at("up to date"),
		chromedp.Focus(codeSection+` .main`, chromedp.ByQuery),
		chromedp.KeyEvent("r"),
		chromedp.Poll(`document.querySelector('`+codeNotice+`')?.dataset.state === 'current'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+codeNotice+`').innerText.replace(/\s+/g, ' ').trim()`, &upToDate),
		chromedp.Evaluate(`document.querySelector('`+codeNotice+`').dataset.state`, &upToDateState),
		chromedp.Poll(`!document.querySelector('`+codeNotice+`')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	)
	if err != nil {
		var dump string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({state: document.querySelector('`+codeBar+`')?.dataset.state, rows: document.querySelectorAll('`+codeRows+`').length, ids: [...document.querySelectorAll('`+codeSection+` section.file')].map(s=>s.id), notice: document.querySelector('`+codeNotice+`')?.innerText})`, &dump))
		t.Fatalf("at %s: %v\n%s\nJS errors: %v", step, err, dump, errs.all())
	}
	saveShot("code-refresh-notice.jpg", shot)

	if sections != 6 {
		t.Fatalf("the diff drew %d sections, want 6", sections)
	}
	if !strings.Contains(head, "7 files") {
		t.Errorf("the list head did not follow: %q", head)
	}
	if word != "2 files changed" {
		t.Errorf("source bar state = %q, want %q", word, "2 files changed")
	}
	if notice != "2 files changed since loaded Reload hunks R" {
		t.Errorf("notice = %q", notice)
	}
	if dots != "README.md,later.txt" {
		t.Errorf("rows with a dot = %q, want README.md and later.txt", dots)
	}
	if dirtyBefore == dirtyAfter {
		t.Errorf("the uncommitted count did not follow: %q", dirtyAfter)
	}
	if len(strayAfterEvent) != 0 || marks != sections {
		t.Errorf("an event moved the hunks: new sections %v, %d of %d kept", strayAfterEvent, marks, sections)
	}
	if placeBefore >= 0 || placeAfter != placeBefore {
		t.Errorf("the hunks moved on an event: the long file's section sat at %v and now at %v", placeBefore, placeAfter)
	}
	if sectionForNew {
		t.Error("a section for the new file was drawn before Reload")
	}

	// Reload: only the README (its hunks differ) and the new file are drawn.
	if len(strayAfterReload) != 2 {
		t.Errorf("sections drawn by Reload = %v, want README.md and later.txt only", strayAfterReload)
	}
	if markedAfterReload != sections-1 {
		t.Errorf("%d sections kept their element, want %d", markedAfterReload, sections-1)
	}
	if placeReloaded < placeBefore-2 || placeReloaded > placeBefore+2 {
		t.Errorf("the place after Reload = %v, was %v", placeReloaded, placeBefore)
	}
	if !regexp.MustCompile(`^updated \d+ s ago$`).MatchString(updated) {
		t.Errorf("file head says %q", updated)
	}
	if noticeAfterReload != "" {
		t.Errorf("a notice is left after Reload: %q", noticeAfterReload)
	}
	if dotsLeft != "" {
		t.Errorf("rows still carry a dot after Reload: %q", dotsLeft)
	}
	if upToDate != "Up to date" || upToDateState != "current" {
		t.Errorf("second Reload says %q (%s), want Up to date", upToDate, upToDateState)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestAStoppedSessionSaysSoAndReadsOnOpenAndOnReload: the source bar reads
// "stopped · as of 12:41", an event changes nothing, Reload reads again.
func TestAStoppedSessionSaysSoAndReadsOnOpenAndOnReload(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionStopped, func(w *codeWorld) string { return w.at.WorkspaceID }))
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code/changes"

	var state, text, afterEvent, afterReload, reloadButton string
	var rowsBefore, rowsEvent, rowsReload int
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		chromedp.Poll(`document.querySelector('`+codeBar+`')?.dataset.state === 'stopped' && document.querySelectorAll('`+codeRows+`').length > 0 && document.querySelector('`+codeSection+` .diff-view .code-lines .line')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+codeBar+`').dataset.state`, &state),
		chromedp.Evaluate(`document.querySelector('`+codeBar+` .state').innerText.replace(/\s+/g, ' ').trim()`, &text),
		chromedp.Evaluate(`document.querySelector('`+codeBar+` [data-reload]')?.innerText.replace(/\s+/g, ' ').trim() ?? ''`, &reloadButton),
		chromedp.Evaluate(`document.querySelectorAll('`+codeRows+`').length`, &rowsBefore),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),

		// An event for a worktree nobody follows changes nothing on the page.
		chromedp.ActionFunc(func(context.Context) error {
			if err := agentWrites(w, "later.txt"); err != nil {
				return err
			}
			w.feed.send(codeChangedMsg(w))
			return nil
		}),
		chromedp.Sleep(2500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('`+codeRows+`').length`, &rowsEvent),
		chromedp.Evaluate(`document.querySelector('`+codeBar+`').dataset.state`, &afterEvent),

		chromedp.Click(codeBar+` [data-reload]`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('`+codeSection+` section.file[id="diff-later-txt"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('`+codeRows+`').length`, &rowsReload),
		chromedp.Evaluate(`document.querySelector('`+codeBar+` .state').innerText.replace(/\s+/g, ' ').trim()`, &afterReload),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	saveShot("code-stopped-session.jpg", shot)
	asOf := regexp.MustCompile(`^stopped as of \d\d:\d\d$`)
	if state != "stopped" || !asOf.MatchString(text) {
		t.Errorf("source bar = %q (%q), want stopped as of HH:MM (the dot between is drawn by CSS)", text, state)
	}
	if reloadButton != "Reload R" {
		t.Errorf("Reload button = %q", reloadButton)
	}
	if rowsEvent != rowsBefore || afterEvent != "stopped" {
		t.Errorf("an event moved a stopped session's view: %d rows then %d, state %q", rowsBefore, rowsEvent, afterEvent)
	}
	if rowsReload != rowsBefore+1 || !asOf.MatchString(afterReload) {
		t.Errorf("Reload: %d rows (was %d), bar %q", rowsReload, rowsBefore, afterReload)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestTheCodeTabsChipAndListFollowGitState: the Code tab has no page watch,
// so its chip follows the feed's git_state itself, and the same event
// refreshes the list.
func TestTheCodeTabsChipAndListFollowGitState(t *testing.T) {
	w := syncWorld(t, aheadOfItsRemote)
	ctx, errs := browser(t)
	var rowsBefore, rowsAfter int
	var chip string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(codeURL(w)+"/changes"),
		pollFor(`document.querySelectorAll('[data-ssr]').length === 0 && `+chipJS+` !== '' && document.querySelectorAll('`+codeRows+`').length > 0`),
		w.feed.connected(),
		chromedp.Evaluate(`document.querySelectorAll('`+codeRows+`').length`, &rowsBefore),
		chromedp.ActionFunc(func(context.Context) error {
			if err := agentWrites(w, "later.txt"); err != nil {
				return err
			}
			behind := w.syncState(syncUpstream("behind", "origin/"+w.f.Branch, 0, 4), 1*time.Minute, [2]int{3, 0})
			b, _ := json.Marshal(map[string]any{"git_state": behind})
			w.feed.send(string(b))
			return nil
		}),
		pollFor(`document.querySelector('`+chipSelector+`').dataset.state === 'behind'`),
		evalString(chipJS, &chip),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	err = chromedp.Run(ctx, chromedp.Poll(`document.querySelectorAll('`+codeRows+`').length === `+fmt.Sprint(rowsBefore+1), nil, chromedp.WithPollingTimeout(8*time.Second)), chromedp.Evaluate(`document.querySelectorAll('`+codeRows+`').length`, &rowsAfter))
	if err != nil {
		t.Fatalf("the list did not follow the git_state: %v (was %d rows)\nJS errors: %v", err, rowsBefore, errs.all())
	}
	if rowsAfter != rowsBefore+1 {
		t.Errorf("a git_state did not refresh the list: %d rows, was %d", rowsAfter, rowsBefore)
	}
	if !strings.HasPrefix(chip, "behind|") || !strings.Contains(chip, "4 behind") {
		t.Errorf("chip after the event = %q", chip)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestRefreshAndReloadStayUnderTheLongTaskCapWith500ChangedFiles is ADR
// 0044's cap held for the new work: with every one of 505 changed files loaded,
// a stream of events refreshes the list without a main-thread task over 100 ms,
// and so does Reload with hunks, which draws only the files that differ.
func TestRefreshAndReloadStayUnderTheLongTaskCapWith500ChangedFiles(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID }))
	writeChangedFiles(t, w.f.Worktree)
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code/changes"

	var tasks []longTask
	var rows, rowsAfter, drawn int
	var notice string
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(longTaskObserver).Do(ctx)
			return err
		}),
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		chromedp.Poll(`window.__firstPageAt > 0`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		// every page loaded: the refresh reads as far as the reader has read
		chromedp.Poll(`(() => {
			const b = document.querySelector(`+"`"+codeSection+` .diff-summary footer.more button`+"`"+`);
			if (b) b.click();
			return !document.querySelector(`+"`"+codeSection+` .diff-summary footer.more`+"`"+`);
		})()`, nil, chromedp.WithPollingTimeout(30*time.Second), chromedp.WithPollingInterval(300*time.Millisecond)),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('`+codeRows+`').length`, &rows),
		chromedp.Evaluate(marksSections, &drawn),
		w.feed.connected(),
		chromedp.Evaluate(`window.__mark = performance.now()`, nil),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if rows < changedFiles {
		t.Fatalf("only %d rows loaded", rows)
	}

	// Ten events, a file added to the stat listing and one taken out each time.
	for i := 0; i < 10; i++ {
		name := filepath.Join(w.f.Worktree, "bulk", fmt.Sprintf("d%02d", i), fmt.Sprintf("added%02d.txt", i))
		if err := os.WriteFile(name, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		w.feed.send(codeChangedMsg(w))
		time.Sleep(700 * time.Millisecond)
	}
	err = chromedp.Run(ctx,
		chromedp.Poll(`document.querySelectorAll('`+codeRows+`').length === `+fmt.Sprint(rows+10), nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+codeNotice+`')?.innerText.replace(/\s+/g, ' ').trim() ?? ''`, &notice),
		chromedp.Evaluate(`window.__longTasks.filter(t => t.at >= window.__mark)`, &tasks),
	)
	if err != nil {
		t.Fatalf("after the events: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.HasPrefix(notice, "10 files changed since loaded") {
		t.Errorf("notice after ten events = %q", notice)
	}
	for _, lt := range tasks {
		t.Logf("long task during refresh: %d ms at %d ms", lt.Ms, lt.At)
		if float64(lt.Ms) > longTaskCapMs {
			t.Errorf("a main-thread task ran %d ms during the list refresh (cap %.0f ms)", lt.Ms, longTaskCapMs)
		}
	}

	// Reload: hunks again for every loaded file; the ten new files are the only sections drawn.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`window.__mark = performance.now()`, nil),
		chromedp.Focus(codeSection+` .main`, chromedp.ByQuery),
		chromedp.KeyEvent("r"),
		chromedp.Poll(`!document.querySelector('`+codeNotice+`') && document.querySelectorAll('`+codeSection+` section.file').length >= `+fmt.Sprint(rows+10), nil, chromedp.WithPollingTimeout(20*time.Second)),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('`+codeRows+`').length`, &rowsAfter),
		chromedp.Evaluate(`window.__longTasks.filter(t => t.at >= window.__mark)`, &tasks),
	)
	if err != nil {
		t.Fatalf("after Reload: %v\nJS errors: %v", err, errs.all())
	}
	for _, lt := range tasks {
		t.Logf("long task during Reload: %d ms at %d ms", lt.Ms, lt.At)
		if float64(lt.Ms) > longTaskCapMs {
			t.Errorf("a main-thread task ran %d ms during Reload (cap %.0f ms)", lt.Ms, longTaskCapMs)
		}
	}
	if rowsAfter != rows+10 {
		t.Errorf("rows after Reload = %d, want %d", rowsAfter, rows+10)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
