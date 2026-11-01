//go:build !windows

package app

import (
	"bytes"
	"fmt"
	"image/png"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The Components tab of the Design system page against the server as it
// runs: each preview is drawn from the tokens in a sandboxed frame. A frame
// without allow-same-origin cannot be read from the page, so what it draws is
// checked on a screenshot of it.

const (
	dsComponentsPanel = `#design-system-panel-components`
	dsFrame           = dsComponentsPanel + ` iframe[data-preview-frame]`
)

func componentsRecord() map[string]any {
	preview := func(label, state, css string) map[string]any {
		return map[string]any{"label": label, "variant": "primary", "state": state, "html": `<p class="fill">Save</p>`, "css": css}
	}
	return map[string]any{
		"tokens":     []any{dsToken("colors", "signal", "color", "#ff0000"), dsToken("colors", "ink", "color", "#00ff00")},
		"principles": "",
		"components": []any{
			map[string]any{"slug": "button", "name": "Button", "purpose": "Starts one action.", "do": []any{"Label with a verb."}, "dont": []any{"Two primaries in a row."},
				"previews": []any{
					preview("Primary / default", "default", `body{margin:0}.fill{margin:0;height:100vh;background:var(--colors-signal)}`),
					preview("Primary / disabled", "disabled", `body{margin:0}.fill{margin:0;height:100vh;background:var(--colors-ink)}`),
				}},
			map[string]any{"slug": "badge", "name": "Badge", "purpose": "A short label.", "do": []any{}, "dont": []any{},
				"previews": []any{preview("Plain", "default", `.fill{color:var(--colors-signal)}`)}},
		},
	}
}

// framePixel is the colour at the centre of the n-th preview frame as drawn.
func framePixel(me *person, t *testing.T, n int) string {
	t.Helper()
	var buf []byte
	me.run(t, "screenshot the frame", chromedp.Screenshot(fmt.Sprintf(`%s [data-preview-card]:nth-of-type(%d) iframe`, dsComponentsPanel, n), &buf, chromedp.ByQuery))
	img, err := png.Decode(bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	r, g, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, bl>>8)
}

func TestDesignSystemComponentsTabShowsEachPreviewDrawnFromTheTokens(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignSystemPage(t)
	pg.mustSave(0, componentsRecord())
	me := newPerson(t)

	me.run(t, "open the Components tab",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(pg.url+"?tab=components"),
		designPoll(`document.querySelectorAll('`+dsComponentsPanel+` [data-component-item]').length === 2`),
		designPoll(`document.querySelector('#design-system-tabs [aria-controls=design-system-panel-components] [data-tab-count]')?.textContent.trim() === '2'`),
		designPoll(`document.querySelector('`+dsComponentsPanel+` [data-component-purpose]')?.textContent.trim() === 'Starts one action.'`),
		designPoll(`document.querySelector('`+dsComponentsPanel+` [data-do] li')?.textContent.trim() === 'Label with a verb.'`),
		designPoll(`document.querySelector('`+dsComponentsPanel+` [data-dont] li')?.textContent.trim() === 'Two primaries in a row.'`),
		designPoll(`document.querySelectorAll('`+dsFrame+`').length === 2`),
		designPoll(`[...document.querySelectorAll('`+dsFrame+`')].every((f) => f.getAttribute('sandbox') === '')`),
		designPoll(`[...document.querySelectorAll('`+dsComponentsPanel+` [data-preview-label]')].map((e) => e.textContent.trim()).join('|') === 'Primary / default|Primary / disabled'`),
	)

	// The previews are drawn from the tokens.
	var first, second string
	for _, c := range []struct {
		n    int
		want string
		got  *string
	}{{1, "#ff0000", &first}, {2, "#00ff00", &second}} {
		eventually(t, "the frame draws the token", func() error {
			*c.got = framePixel(me, t, c.n)
			if *c.got != c.want {
				return fmt.Errorf("frame %d is %s, want %s", c.n, *c.got, c.want)
			}
			return nil
		})
	}
	me.run(t, "screenshot", shot("components", "light"))

	// A token changed on the Tokens tab, unsaved, changes the preview.
	me.run(t, "change a token without saving",
		designClick(`#design-system-tabs [role=tab][aria-controls=design-system-panel-tokens]`),
		chromedp.SetValue(tokenRow(1, `[data-field=value]`), "#0000ff", chromedp.ByQuery),
		designClick(`#design-system-tabs [role=tab][aria-controls=design-system-panel-components]`),
		designPoll(`document.querySelectorAll('`+dsFrame+`').length === 2`),
	)
	eventually(t, "the unsaved token reaches the preview", func() error {
		if got := framePixel(me, t, 1); got != "#0000ff" {
			return fmt.Errorf("frame 1 is %s", got)
		}
		return nil
	})

	// A token renamed on the Tokens tab, unsaved, leaves a preview naming a
	// token that no longer exists: flagged, naming it.
	me.run(t, "the missing token is named",
		designClick(`#design-system-tabs [role=tab][aria-controls=design-system-panel-tokens]`),
		chromedp.SetValue(tokenRow(2, `[data-field=name]`), "ink-renamed", chromedp.ByQuery),
		designClick(`#design-system-tabs [role=tab][aria-controls=design-system-panel-components]`),
		designPoll(`document.querySelector('`+dsComponentsPanel+` [data-missing-token]')?.textContent.includes('--colors-ink')`),
		designPoll(`document.querySelector('`+dsComponentsPanel+` [data-component-item][data-slug=button] [data-flag]') !== null`),
		designPoll(`document.querySelector('`+dsComponentsPanel+` [data-component-item][data-slug=badge] [data-flag]') === null`),
		shot("components-missing-token", "light"),
	)
}

func TestDesignSystemComponentsTabNeverRunsAScriptInAPreview(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	pg := startDesignSystemPage(t)
	pg.mustSave(0, componentsRecord())
	me := newPerson(t)
	evil := `[{slug:'evil',name:'Evil',purpose:'',do:[],dont:[],previews:[{label:'x',variant:'a',state:'b',css:'',` +
		`html:'<script>parent.postMessage("ran","*")</script><img src="x" onerror="parent.postMessage(\'ran\',\'*\')">'}]}]`

	me.run(t, "a preview with a script, set the way the page holds components",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(pg.url+"?tab=components"),
		designPoll(`document.querySelectorAll('`+dsFrame+`').length === 2`),
		chromedp.Evaluate(`window.__ran = false; window.addEventListener('message', () => { window.__ran = true }); document.querySelector('main')._x_dataStack[0].components = `+evil, nil),
		designPoll(`document.querySelector('`+dsFrame+`')?.getAttribute('srcdoc')?.includes('<script>')`),
		chromedp.Sleep(500*1e6),
	)
	var ran bool
	var sandbox string
	me.run(t, "nothing ran", chromedp.Evaluate(`window.__ran`, &ran), chromedp.Evaluate(`document.querySelector('`+dsFrame+`').getAttribute('sandbox')`, &sandbox))
	if ran {
		t.Error("a script in a preview ran")
	}
	if sandbox != "" || strings.Contains(sandbox, "allow") {
		t.Errorf("sandbox = %q", sandbox)
	}
}
