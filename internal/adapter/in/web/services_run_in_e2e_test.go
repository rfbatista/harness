//go:build !windows

package web_test

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A command typed in the Subfolder field is refused when the service is saved,
// under that field, in the server's words (spec "Web UI — services: Run in
// picker and run variables", testing decisions; contract section 11).
func TestACommandTypedInTheSubfolderFieldIsRefusedWhenSaved_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	b := newTLSBrowser(t)
	b.run("open the form on the Services page",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!document.querySelector('[data-services-empty]')`),
		chromedp.Click(`[data-new-service]`, chromedp.ByQuery),
		waitUntil(`document.activeElement.id === 'sf-name'`),
	)
	b.run("fill it with a repository and a command in the subfolder",
		chromedp.SendKeys(`#sf-name`, "web", chromedp.ByQuery),
		chromedp.SendKeys(`#sf-line`, "make air", chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const s = document.querySelector('#sf-repository'); s.value = '`+w.repo.ID+`'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
		waitUntil(`document.querySelector('#sf-subdir').closest('.field').checkVisibility()`),
		waitUntil(`document.getElementById('sf-subdir-hint').textContent.includes('not a command')`),
		chromedp.SendKeys(`#sf-subdir`, "npm run dev", chromedp.ByQuery),
		chromedp.Click(`[data-form-save]`, chromedp.ByQuery),
	)
	b.run("the refusal shows under the Subfolder field and nothing is saved",
		waitUntil(`(() => { const e = document.getElementById('sf-subdir-error'); return !e.hidden && e.textContent.includes('"npm run dev" is not a folder in api') && document.getElementById('sf-subdir').getAttribute('aria-invalid') === 'true'; })()`),
		waitUntil(`!!document.querySelector('[data-service-form]') && !document.querySelector('[data-service]')`),
	)
}
