//go:build !windows

package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

const poOwnerPage = `document.querySelector('.owner-page .owner-split')`

// A product owner's answer is Markdown, and what the person typed comes back in it: a
// javascript: link and raw HTML must show as text, never as a link or as markup.
func TestTheProductOwnerAnswerShowsAJavascriptLinkAndRawHTMLAsTextInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	p := newPerson(t)
	const hostile = `[click](javascript:alert(1)) <b>loud</b> <img src=x onerror=alert(2)>`

	p.run(t, "the panel opens and the hostile text is sent",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		chromedp.Click(`.po-toggle`, chromedp.ByQuery),
		waitFor(`document.activeElement?.id === 'po-composer'`),
		chromedp.SendKeys(`#po-composer`, hostile, chromedp.ByQuery),
		pressEnter(),
		waitFor(poMessage+`.some(m => m.dataset.from === 'product_owner' && !m.hasAttribute('data-streaming') && m.textContent.includes('answer to: '))`),
	)
	for _, bad := range []string{
		`.owner-panel .msg a[href^="javascript" i]`,
		`.owner-panel .msg b`,
		`.owner-panel .msg img`,
		`.owner-panel .msg [onerror]`,
	} {
		if p.truth(t, `!!document.querySelector('`+bad+`')`) {
			t.Errorf("the panel renders %s from the message text", bad)
		}
	}
	answer := p.text(t, `[...document.querySelectorAll('.owner-panel .msg[data-from=product_owner] .body')].map(e => e.textContent).join('\n')`)
	if !strings.Contains(answer, "<b>loud</b>") {
		t.Errorf("the answer's text = %q, want the raw <b>loud</b> kept as text", answer)
	}
	if !strings.Contains(answer, "click") {
		t.Errorf("the answer's text = %q, want the link's label kept", answer)
	}
}

// Stop in the middle of an answer: the partial answer stays, marked stopped, and the composer
// goes back to Send.
func TestAStoppedProductOwnerAnswerStaysMarkedStoppedInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	p := newPerson(t)
	w.hold()
	defer w.release()

	p.run(t, "a message is sent while the stand-in claude holds mid-answer",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		chromedp.Click(`.po-toggle`, chromedp.ByQuery),
		waitFor(`document.activeElement?.id === 'po-composer'`),
		chromedp.SendKeys(`#po-composer`, "tell me everything", chromedp.ByQuery),
		pressEnter(),
		waitFor(poMessage+`.some(m => m.dataset.from === 'product_owner' && m.hasAttribute('data-streaming'))`),
		waitFor(`document.querySelector('.composer .send')?.getAttribute('aria-label') === 'Stop answering'`),
	)
	p.run(t, "Stop is clicked",
		chromedp.Click(`.composer .send`, chromedp.ByQuery),
		waitFor(poMessage+`.some(m => m.dataset.from === 'product_owner' && !m.hasAttribute('data-streaming') && m.textContent.includes('stopped'))`),
		waitFor(`document.querySelector('.composer .send')?.getAttribute('aria-label') === 'Send'`),
	)
	w.release()

	p.run(t, "the stopped answer is still there after the page is read again",
		chromedp.Navigate(w.url+"/projects/"+w.project),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		waitFor(`!!`+poPanel+` && `+poMessage+`.some(m => m.dataset.from === 'product_owner' && m.textContent.includes('stopped'))`),
	)
	if n := p.text(t, `String(`+poMessage+`.filter(m => m.dataset.from === 'product_owner' && m.textContent.includes('stopped')).length)`); n != "1" {
		t.Errorf("%s stopped answers after the reload, want 1", n)
	}
}

// A thread of 130 messages opens on the last hundred; "Earlier messages" adds the other thirty,
// in order, each once.
func TestEarlierMessagesPageBackAThreadWithoutRepeatsInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	long := w.seedThread("A long sitting", time.Now().Add(-time.Hour), 130)
	p := newPerson(t)

	p.run(t, "the thread opens on its last hundred messages",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project+"/product-owner/threads/"+long),
		waitFor(`!!`+poOwnerPage),
		waitFor(poMessage+`.length === 100`),
	)
	first := p.text(t, poMessage+`[0].querySelector('.body').textContent.trim()`)
	if first != "message 31" {
		t.Errorf("the first message shown = %q, want message 31", first)
	}
	p.run(t, "Earlier messages is clicked",
		chromedp.Click(`.older`, chromedp.ByQuery),
		waitFor(poMessage+`.length === 130`),
		waitFor(`!document.querySelector('.older')`),
	)
	got := p.text(t, poMessage+`.map(m => m.querySelector('.body').textContent.trim()).join('|')`)
	var want []string
	for i := 1; i <= 130; i++ {
		want = append(want, fmt.Sprintf("message %d", i))
	}
	if got != strings.Join(want, "|") {
		t.Errorf("the thread after paging back = %.200s…, want message 1 to message 130 once each, in order", got)
	}
}

// The person renames a thread in About; a new answer must not take the title back.
func TestARenameFromAboutIsKeptAfterANewAnswerInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	p := newPerson(t)

	p.run(t, "a thread starts in the panel",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		chromedp.Click(`.po-toggle`, chromedp.ByQuery),
		waitFor(`document.activeElement?.id === 'po-composer'`),
		chromedp.SendKeys(`#po-composer`, "first question", chromedp.ByQuery),
		pressEnter(),
		waitFor(poMessage+`.some(m => m.textContent.includes('answer to: first question') && !m.hasAttribute('data-streaming'))`),
	)
	p.run(t, "About renames it",
		chromedp.Click(`.owner-panel [aria-label="About this thread"]`, chromedp.ByQuery),
		waitFor(`!![...document.querySelectorAll('.about button')].find(b => b.textContent.trim() === 'Rename')`),
		chromedp.Evaluate(`[...document.querySelectorAll('.about button')].find(b => b.textContent.trim() === 'Rename').click()`, nil),
		waitFor(`!!document.querySelector('#po-rename')`),
		setValue(`#po-rename`, "Release plan"),
		chromedp.Click(`.about button[type=submit]`, chromedp.ByQuery),
		waitFor(`document.querySelector('.owner-panel .owner-head .thread')?.textContent === 'Release plan'`),
	)
	p.run(t, "a second message is answered",
		chromedp.Focus(`#po-composer`, chromedp.ByQuery),
		chromedp.SendKeys(`#po-composer`, "second question", chromedp.ByQuery),
		pressEnter(),
		waitFor(poMessage+`.some(m => m.textContent.includes('answer to: second question') && !m.hasAttribute('data-streaming'))`),
	)
	if got := p.text(t, `document.querySelector('.owner-panel .owner-head .thread')?.textContent ?? ''`); got != "Release plan" {
		t.Errorf("the title after the new answer = %q, want Release plan", got)
	}
	p.run(t, "the page's thread list shows the person's title after a reload",
		chromedp.Navigate(w.url+"/projects/"+w.project+"/product-owner"),
		waitFor(`!!`+poOwnerPage),
		waitFor(`[...document.querySelectorAll('.owner-split > .threads .thread-row .t')].some(e => e.textContent.trim() === 'Release plan')`),
	)
}

// The panel's thread list filters by title and by excerpt.
func TestThePanelsThreadFilterNarrowsTheListInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	w.seedThread("Roadmap ideas", time.Now().Add(-48*time.Hour), 2)
	w.seedThread("What blocks the release", time.Now().Add(-time.Hour), 2)
	p := newPerson(t)
	rows := `[...document.querySelectorAll('.popup .thread-row .t')].map(e => e.textContent.trim())`

	p.run(t, "the panel's Threads popup opens",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		chromedp.Click(`.po-toggle`, chromedp.ByQuery),
		waitFor(`document.activeElement?.id === 'po-composer'`),
		chromedp.Click(`.owner-panel [aria-label="Threads"]`, chromedp.ByQuery),
		waitFor(rows+`.length === 2`),
	)
	p.run(t, "a title filter keeps one thread",
		chromedp.SendKeys(`#po-thread-filter-popup`, "ROADMAP", chromedp.ByQuery),
		waitFor(rows+`.join('|') === 'Roadmap ideas'`),
	)
	p.run(t, "a filter that matches nothing says so, and clearing it brings both back",
		setValue(`#po-thread-filter-popup`, "zzz-nothing"),
		waitFor(rows+`.length === 0`),
		setValue(`#po-thread-filter-popup`, ""),
		waitFor(rows+`.length === 2`),
	)
}

// Under 64rem the toggle opens the page; under 48rem the page shows the list first, a thread
// opens in place of it, and Back returns to the list.
func TestOnNarrowScreensTheToggleOpensThePageAndAPhoneShowsTheListFirstInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	id := w.seedThread("Roadmap ideas", time.Now().Add(-time.Hour), 2)
	p := newPerson(t)
	board := w.url + "/projects/" + w.project

	p.run(t, "under 64rem the toggle opens the page, not a panel",
		chromedp.EmulateViewport(1000, 800),
		chromedp.Navigate(board),
		waitFor(poLive+` && document.querySelector('[data-ssr]') === null`),
		waitFor(`!!`+poToggle),
		chromedp.Click(`.po-toggle`, chromedp.ByQuery),
		waitFor(`location.pathname === '/projects/`+w.project+`/product-owner' && !!`+poOwnerPage),
	)
	if p.truth(t, `!!`+poPanel) {
		t.Error("a panel docks on a screen under 64rem")
	}

	p.run(t, "under 48rem the list comes first",
		chromedp.EmulateViewport(600, 900),
		chromedp.Navigate(board+"/product-owner"),
		waitFor(`!!`+poOwnerPage),
		waitFor(`document.querySelector('.owner-split > .threads')?.checkVisibility()`),
	)
	if p.truth(t, `!!document.querySelector('.owner-split .conversation')?.checkVisibility()`) {
		t.Error("the conversation shows beside the list on a phone")
	}
	p.run(t, "a thread opens in place of the list",
		chromedp.Click(`.owner-split > .threads .thread-row`, chromedp.ByQuery),
		waitFor(`decodeURIComponent(location.pathname) === '/projects/`+w.project+`/product-owner/threads/`+id+`'`),
		waitFor(poMessage+`.length === 2`),
		waitFor(`!document.querySelector('.owner-split > .threads')?.checkVisibility()`),
	)
	p.run(t, "Back returns to the list",
		chromedp.Click(`[aria-label="Back to threads"]`, chromedp.ByQuery),
		waitFor(`document.querySelector('.owner-split > .threads')?.checkVisibility()`),
		waitFor(`!document.querySelector('.owner-split .conversation')?.checkVisibility()`),
	)
}

// A thread address of another project shows the notice and starts nothing.
func TestAThreadAddressOfAnotherProjectShowsTheNoticeInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	other, err := w.cat.Projects.CreateProject(w.ctx, "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign := "foreign-thread"
	if err := w.store.CreateThread(w.ctx, domain.ProductOwnerThread{ID: foreign, ProjectID: other.ID, SessionID: "none-foreign", Title: "Not yours",
		Turn: domain.ProductOwnerTurnIdle, StartedAt: time.Now(), LastActivityAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	p := newPerson(t)

	p.run(t, "the other project's thread opened under this project",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project+"/product-owner/threads/"+foreign),
		waitFor(`!!`+poOwnerPage),
		waitFor(`!!document.querySelector('.owner-page [role=alert]') && document.querySelector('.owner-page [role=alert]').textContent.includes('not a thread of this project')`),
	)
	if p.truth(t, `document.body.textContent.includes('Not yours') && !!`+poMessage+`.length`) {
		t.Error("the other project's thread shows under this project")
	}
}

// A failure item offers Try again, which sends the same text again.
func TestTryAgainOnAFailedProductOwnerAnswerSendsTheSameTextInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	id := w.seedThread("Failed one", time.Now().Add(-time.Hour), 1)
	asked, err := w.store.AppendItem(w.ctx, domain.ProductOwnerItem{ThreadID: id, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
		Author: domain.ProductOwnerAuthorPerson, Text: "retry me please"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.AppendItem(w.ctx, domain.ProductOwnerItem{ThreadID: id, Kind: domain.ProductOwnerItemFailure, At: time.Now(),
		Code: "SESSION_RESUME_FAILED", Text: "The session did not wake.", RetryItemID: asked.ID}); err != nil {
		t.Fatal(err)
	}
	p := newPerson(t)
	count := poMessage + `.filter(m => m.dataset.from === 'person' && m.textContent.includes('retry me please')).length`

	p.run(t, "the failure shows its banner, its code and Try again",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project+"/product-owner/threads/"+id),
		waitFor(`!!`+poOwnerPage),
		waitFor(`!![...document.querySelectorAll('.owner-page .banner')].find(b => b.textContent.includes('SESSION_RESUME_FAILED') && b.textContent.includes('Try again'))`),
		waitFor(count+` === 1`),
	)
	p.run(t, "Try again sends the same message again",
		chromedp.Click(`.owner-page .banner button`, chromedp.ByQuery),
		waitFor(count+` === 2`),
	)
}

// setValue types into a React-controlled input the way the browser does: the native setter,
// then an input event, so the component's onChange sees it.
func setValue(selector, value string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const el = document.querySelector(`+jsString(selector)+`); `+
		`Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, `+jsString(value)+`); `+
		`el.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
}
