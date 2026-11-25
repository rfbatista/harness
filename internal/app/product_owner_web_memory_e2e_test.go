//go:build !windows

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The product owner's memory in the browser, against the real server (ADR 0111): the product
// owner saves an entry through its tool, the thread shows the note, an open Memory tab shows
// the entry arriving, the person edits one in place and deletes another behind a danger
// confirm, and a fresh thread's intro counts what is left.
func TestTheMemoryTabAndTheNoteInTheThreadFollowTheProductOwnersMemoryInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	threadID, owner := w.ownerOf("we decided to ship on fridays")
	first := w.saveMemory(owner, "Ship on Fridays.")
	p := newPerson(t)
	page := w.url + "/projects/" + w.project + "/product-owner"

	entry := func(text string) string {
		return `[...document.querySelectorAll('.memory-entry')].find(e => e.textContent.includes(` + jsString(text) + `))`
	}
	notes := `[...document.querySelectorAll('.owner-page .msg-note')]`

	p.run(t, "the thread shows the product owner's \"Saved to memory\" note, with the entry and an Edit link",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(page+"/threads/"+threadID),
		waitFor(`!!document.querySelector('.owner-page .owner-split')`),
		waitFor(notes+`.some(n => n.dataset.kind === 'saved' && n.textContent.includes('Saved to memory') && n.textContent.includes('Ship on Fridays.') && !!n.querySelector('a.act'))`),
	)

	p.run(t, "the Memory tab lists the entry with its thread and the count",
		chromedp.Click(`.owner-page [role=tab][href$="#memory"]`, chromedp.ByQuery),
		waitFor(`!!`+entry("Ship on Fridays.")),
		waitFor(`document.querySelector('.owner-page [role=tab][aria-selected=true]')?.textContent.startsWith('Memory') && document.querySelector('.owner-page [role=tab][aria-selected=true] .badge')?.textContent === '1'`),
	)
	if got := p.text(t, entry("Ship on Fridays.")+`.querySelector('.meta').textContent`); !strings.Contains(got, "Saved ") || !strings.Contains(got, "from ") {
		t.Errorf("the entry's meta line says %q, want when it was saved and the thread it came from", got)
	}

	second := w.saveMemory(owner, "Env files never leave the machine.")
	p.run(t, "an entry the product owner saves arrives on the open tab, fresh, and the count follows",
		waitFor(`!!`+entry("Env files never leave")+` && `+entry("Env files never leave")+`.hasAttribute('data-fresh')`),
		waitFor(`document.querySelector('.owner-page [role=tab][aria-selected=true] .badge')?.textContent === '2'`),
	)
	if second.EntryID == "" {
		t.Fatal("no second entry")
	}

	p.run(t, "Edit turns the entry into a text area, and Save sends the person's text",
		chromedp.Evaluate(entry("Ship on Fridays.")+`.querySelector('[data-act=edit]').click()`, nil),
		waitFor(`document.activeElement?.tagName === 'TEXTAREA' && !!document.querySelector('.memory-entry[data-editing]')`),
		chromedp.Evaluate(`(() => { const t = document.querySelector('.memory-entry[data-editing] textarea'); Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(t, 'Ship on Thursdays.'); t.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil),
		waitFor(`!document.querySelector('.memory-entry[data-editing] button[type=submit]').disabled`),
		chromedp.Click(`.memory-entry[data-editing] button[type=submit]`, chromedp.ByQuery),
		waitFor(`!document.querySelector('.memory-entry[data-editing]') && !!`+entry("Ship on Thursdays.")),
		waitFor(entry("Ship on Thursdays.")+`.querySelector('.meta').textContent.includes('edited by you')`),
	)
	var read struct {
		Entries []memoryEntry `json:"entries"`
	}
	if status, code := w.get("product_owner_memory?project_id="+w.project, &read); status != 200 {
		t.Fatalf("GET memory = %d %s", status, code)
	}
	var edited *memoryEntry
	for i := range read.Entries {
		if read.Entries[i].EntryID == first.EntryID {
			edited = &read.Entries[i]
		}
	}
	if edited == nil || edited.Text != "Ship on Thursdays." || edited.ChangedBy == nil || *edited.ChangedBy != "person" {
		t.Errorf("the server holds %+v, want the person's edit", edited)
	}

	p.run(t, "Delete asks first; Keep it changes nothing; Delete entry leaves a receipt",
		chromedp.Evaluate(entry("Env files never leave")+`.querySelector('[data-act=delete]').click()`, nil),
		waitFor(`!!document.querySelector('.memory-entry .confirm-strip[data-tone=danger]') && document.activeElement?.textContent === 'Keep it'`),
		chromedp.Click(`.memory-entry .confirm-strip [data-act=keep]`, chromedp.ByQuery),
		waitFor(`!document.querySelector('.confirm-strip') && !!`+entry("Env files never leave")),
		chromedp.Evaluate(entry("Env files never leave")+`.querySelector('[data-act=delete]').click()`, nil),
		waitFor(`!!document.querySelector('.memory-entry .confirm-strip')`),
		chromedp.Click(`.memory-entry .confirm-strip [data-variant=danger]`, chromedp.ByQuery),
		waitFor(`!!document.querySelector('.memory-entry[data-receipt]') && document.querySelector('.memory-entry[data-receipt]').textContent.includes('Deleted')`),
		waitFor(`document.querySelector('.owner-page [role=tab][aria-selected=true] .badge')?.textContent === '1'`),
	)
	if status, code := w.get("product_owner_memory?project_id="+w.project, &read); status != 200 || len(read.Entries) != 1 {
		t.Errorf("GET memory = %d %s with %d entries, want the delete to have reached the server", status, code, len(read.Entries))
	}

	p.run(t, "the filter narrows the entries by their text",
		chromedp.SendKeys(`#po-memory-filter`, "nothing like this", chromedp.ByQuery),
		waitFor(`document.querySelector('.memory-empty')?.textContent.includes('No entry matches')`),
	)

	p.run(t, "a fresh thread's intro counts what the product owner remembers",
		chromedp.Click(`.owner-page .toolbar .actions .button`, chromedp.ByQuery),
		waitFor(`document.querySelector('.owner-page .thread-empty .remembered')?.textContent === 'I remember 1 thing we agreed'`),
	)

	p.run(t, "a note saved while the thread is open arrives live, and its Edit link opens the Memory tab at the entry",
		chromedp.Navigate(page+"/threads/"+threadID),
		waitFor(notes+`.length === 2`),
		// The page's stream follows what it is told after the thread is read.
		chromedp.Sleep(time.Second),
	)
	live := w.saveMemory(owner, "Reviews are on Mondays.")
	p.run(t, "the new note shows and its Edit link opens the entry",
		waitFor(notes+`.length === 3 && `+notes+`.some(n => n.textContent.includes('Reviews are on Mondays.'))`),
		chromedp.Evaluate(notes+`.find(n => n.textContent.includes('Reviews are on Mondays.')).querySelector('a.act').click()`, nil),
		waitFor(`!!`+entry("Reviews are on Mondays.")+` && document.activeElement?.id === 'memory-entry-`+live.EntryID+`'`),
	)

	p.run(t, "a deleted last entry leaves the empty state on the next visit",
		chromedp.Evaluate(entry("Ship on Thursdays.")+`.querySelector('[data-act=delete]').click()`, nil),
		chromedp.Click(`.memory-entry .confirm-strip [data-variant=danger]`, chromedp.ByQuery),
		waitFor(`!!document.querySelector('.memory-entry[data-receipt]')`),
		chromedp.Evaluate(entry("Reviews are on Mondays.")+`.querySelector('[data-act=delete]').click()`, nil),
		chromedp.Click(`.memory-entry .confirm-strip [data-variant=danger]`, chromedp.ByQuery),
		waitFor(`document.querySelectorAll('.memory-entry[data-receipt]').length === 2`),
		// The Memory tab wrote its own address when the Edit link opened it, so the page is already at #memory.
		chromedp.Reload(),
		waitFor(`document.querySelector('.memory-empty h2')?.textContent === 'Nothing remembered yet'`),
	)
}
