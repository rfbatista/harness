//go:build !windows

package app

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A real mouse click on an entry's Edit button opens the edit, and never the Delete confirm.
// The Delete button sits right beside Edit: nothing of it may cover Edit's face. A click
// dispatched from script (element.click()) would not see an overlap, so this one clicks with
// the mouse, at the centre of the button, as a person does.
func TestAMouseClickOnAMemoryEntrysEditButtonOpensTheEditNotTheDeleteConfirmInTheWebUI_EndToEnd(t *testing.T) {
	w := newPOWeb(t)
	_, owner := w.ownerOf("we decided to ship on fridays")
	w.saveMemory(owner, "Ship on Fridays.")
	p := newPerson(t)

	p.run(t, "the Memory tab lists the entry",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project+"/product-owner#memory"),
		waitFor(`!!document.querySelector('.memory-entry [data-act=edit]')`),
	)

	p.run(t, "the mouse lands on Edit, not on Delete",
		waitFor(`(() => { const b = document.querySelector('.memory-entry [data-act=edit]').getBoundingClientRect(); return document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2)?.closest('button')?.dataset.act === 'edit'; })()`),
	)

	p.run(t, "a click at the centre of Edit opens the edit and no confirm",
		chromedp.Click(`.memory-entry [data-act=edit]`, chromedp.ByQuery),
		waitFor(`!!document.querySelector('.memory-entry[data-editing]') && !document.querySelector('.confirm-strip')`),
	)
}
