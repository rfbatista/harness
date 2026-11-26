package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/evanw/esbuild/pkg/api"
)

// The session memory chip (web/app, features/session-memory) in headless Chrome: the real ChipView on the real
// ChipPresenter over fake ports, bundled here with esbuild, with the design system's stylesheet. It proves what
// the presenter tests cannot: the popover opens, and light dismiss and focus work.

// memoryChipPage serves the harness page and returns its address.
func memoryChipPage(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	result := api.Build(api.BuildOptions{
		AbsWorkingDir: filepath.Join(root, "web", "app"),
		EntryPoints:   []string{"src/features/session-memory/testing/browser-harness.tsx"},
		Bundle:        true,
		Write:         false,
		Outfile:       "harness.js",
		Format:        api.FormatIIFE,
		Target:        api.ES2022,
		JSX:           api.JSXAutomatic,
		Define:        map[string]string{"process.env.NODE_ENV": `"production"`},
		LogLevel:      api.LogLevelSilent,
	})
	if len(result.Errors) > 0 || len(result.OutputFiles) == 0 {
		t.Fatalf("bundling the harness: %v", result.Errors)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/harness.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(result.OutputFiles[0].Contents)
	})
	mux.HandleFunc("/harness.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="/design-system/css/index.css"></head><body>` +
			// The session head as screen.templ builds it (state, who, tokens, sandbox chips, the chip, the actions), in a pane that is the "session" container.
			`<div id="pane" style="container-type:inline-size;container-name:session;inline-size:1000px"><header class="session-head">` +
			`<h2>Memory budget QA with a rather long title</h2><p class="meta"><span class="status" data-state="running">running</span>` +
			`<span class="who">frontend-developer · 3m</span><span class="usage-total">11K tokens · $0.42 estimated</span>` +
			`<span class="sandbox-chips"><button class="badge" data-sandbox>Sandboxed</button><button class="badge" data-sandbox-visibility>Own worktree only</button></span>` +
			`<span data-memory-chip><span data-island="session-memory" id="root"></span></span></p>` +
			`<div class="actions"><a class="button" data-variant="ghost" data-size="sm" data-wide href="#">Git history</a><button class="button" data-size="sm">Stop</button>` +
			`<button class="button" data-variant="ghost" data-size="sm" data-icon aria-label="More session actions">⋯</button></div></header></div>` +
			`<button id="elsewhere" style="position:fixed;inset-block-end:1rem;inset-inline-end:1rem">elsewhere</button><script src="/harness.js"></script></body></html>`))
	})
	mux.Handle("/", noStore(http.FileServer(http.Dir(root))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/harness.html"
}

const (
	popoverOpen = `(() => { const p = document.getElementById('memory-popover'); return !!p && p.matches(':popover-open') && getComputedStyle(p).display !== 'none'; })()`
	chipThere   = `!!document.querySelector('.memory:not([hidden])')`
)

func TestMemoryChipPopoverOpensInTheBrowser(t *testing.T) {
	ctx, log := browser(t)
	var open, expanded bool
	var text string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(memoryChipPage(t)),
		chromedp.Poll(`!!window.memoryHarness`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.memoryHarness.release()`, nil),
		chromedp.Poll(chipThere, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(".memory", chromedp.ByQuery),
		chromedp.Poll(popoverOpen, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(popoverOpen, &open),
		chromedp.Evaluate(`document.querySelector('.memory').getAttribute('aria-expanded') === 'true'`, &expanded),
		chromedp.Evaluate(`document.getElementById('memory-popover').textContent`, &text),
	)
	if err != nil {
		t.Fatalf("the popover did not open from the chip: %v", err)
	}
	if !open || !expanded {
		t.Errorf("open = %v, aria-expanded = %v", open, expanded)
	}
	if want := "Memory · 1.2 GB of 4 GB"; len(text) < len(want) || text[:len(want)] != want {
		t.Errorf("popover text = %q", text)
	}
	if errs := log.all(); len(errs) > 0 {
		t.Errorf("page errors: %v", errs)
	}
}

func TestMemoryChipPopoverOpensFromTheToastsDetails(t *testing.T) {
	ctx, _ := browser(t)
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(memoryChipPage(t)),
		chromedp.Poll(`!!window.memoryHarness`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.memoryHarness.release()`, nil),
		chromedp.Poll(chipThere, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.memoryHarness.kill(7)`, nil),
		chromedp.Poll(`!!document.querySelector('.toast')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`.toast .actions button`, chromedp.ByQuery),
		chromedp.Poll(popoverOpen, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("Details did not open the popover: %v", err)
	}
}

func TestMemoryChipPopoverClosesWithEscAndOutsideClickAndGivesFocusBack(t *testing.T) {
	ctx, _ := browser(t)
	var onChip bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(memoryChipPage(t)),
		chromedp.Poll(`!!window.memoryHarness`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.memoryHarness.release()`, nil),
		chromedp.Poll(chipThere, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(".memory", chromedp.ByQuery),
		chromedp.Poll(popoverOpen, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.KeyEvent("\u001b"),
		chromedp.Poll(`!document.getElementById('memory-popover').matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.activeElement === document.querySelector('.memory')`, &onChip),
	)
	if err != nil {
		t.Fatalf("Esc: %v", err)
	}
	if !onChip {
		t.Error("Esc did not give the focus back to the chip")
	}
	// Outside click on a spot that takes no focus: closes, and the focus returns to the chip.
	err = chromedp.Run(ctx,
		chromedp.Click(".memory", chromedp.ByQuery),
		chromedp.Poll(popoverOpen, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var point []float64
			if err := chromedp.Evaluate(`[document.documentElement.clientWidth - 5, document.documentElement.clientHeight / 2]`, &point).Do(ctx); err != nil {
				return err
			}
			return chromedp.MouseClickXY(point[0], point[1]).Do(ctx)
		}),
		chromedp.Poll(`!document.getElementById('memory-popover').matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.activeElement === document.querySelector('.memory')`, &onChip),
	)
	if err != nil {
		t.Fatalf("outside click: %v", err)
	}
	if !onChip {
		t.Error("an outside click on nothing focusable did not give the focus back to the chip")
	}
	// An outside click on another control keeps the focus where the click put it.
	var onOther bool
	err = chromedp.Run(ctx,
		chromedp.Click(".memory", chromedp.ByQuery),
		chromedp.Poll(popoverOpen, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click("#elsewhere", chromedp.ByQuery),
		chromedp.Poll(`!document.getElementById('memory-popover').matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.activeElement === document.getElementById('elsewhere')`, &onOther),
	)
	if err != nil {
		t.Fatalf("outside click on a control: %v", err)
	}
	if !onOther {
		t.Error("the focus was taken from the control the click landed on")
	}
}

// The head widths of the session pane in a 900 px tall window with the sidebar open (15rem) and the session list beside it
// (26rem at most): the viewport less both. 1440 → 784 px (Git history shows), 1280 → 624 px, 1024 → 368 px.
var headWidths = []int{784, 624, 368}

// The head keeps its actions whole and the chip legible: the actions never shrink or get covered, the chip's value
// never shrinks below its content, and only its word gives way.
func TestMemoryChipNeverClipsTheHeadActions(t *testing.T) {
	states := map[string]string{"normal": "gb=1.2", "near": "gb=3.4", "over": "gb=4.5", "ended": "gb=1.2&kill=1"}
	for _, width := range headWidths {
		for name, query := range states {
			t.Run(fmt.Sprintf("%dpx %s", width, name), func(t *testing.T) {
				ctx, _ := browser(t)
				var problems string
				err := chromedp.Run(ctx,
					chromedp.EmulateViewport(1600, 800),
					chromedp.Navigate(memoryChipPage(t)+"?"+query),
					chromedp.Poll(`!!window.memoryHarness`, nil, chromedp.WithPollingTimeout(10*time.Second)),
					chromedp.Evaluate(fmt.Sprintf(`document.getElementById('pane').style.inlineSize = '%dpx'`, width), nil),
					chromedp.Evaluate(`window.memoryHarness.release()`, nil),
					chromedp.Poll(chipThere, nil, chromedp.WithPollingTimeout(10*time.Second)),
					chromedp.Evaluate(headProblems, &problems),
				)
				if err != nil {
					t.Fatal(err)
				}
				if problems != "" {
					t.Errorf("the head: %s", problems)
				}
			})
		}
	}
}

// headProblems says what is wrong with the head, "" when nothing is: it overflows, an action is clipped or covered,
// or the chip's value is cut, covered, or has no room of its own.
const headProblems = `(() => {
	const head = document.querySelector('.session-head');
	const right = head.getBoundingClientRect().right;
	const out = [];
	if (head.scrollWidth > head.clientWidth + 1) out.push('head scrollWidth ' + head.scrollWidth + ' > clientWidth ' + head.clientWidth);
	const actions = [...head.querySelectorAll('.actions > *')].filter(el => el.getBoundingClientRect().width > 0);
	for (const el of actions) {
		const r = el.getBoundingClientRect();
		if (r.right > right + 1) out.push(el.textContent.trim() + ' ends at ' + Math.round(r.right) + ' past ' + Math.round(right));
	}
	const chip = head.querySelector('.memory:not([hidden])');
	if (!chip || getComputedStyle(chip).display === 'none') return out.join('; ');
	const box = chip.getBoundingClientRect();
	const num = chip.querySelector('.num');
	const numBox = num.getBoundingClientRect();
	if (num.scrollWidth > num.clientWidth + 1) out.push('value is cut: scrollWidth ' + num.scrollWidth + ' > clientWidth ' + num.clientWidth);
	if (chip.scrollWidth > chip.clientWidth + 1 && !chip.querySelector('.word')) out.push('chip overflows: scrollWidth ' + chip.scrollWidth + ' > clientWidth ' + chip.clientWidth);
	if (box.width + 0.5 < numBox.width) out.push('chip box ' + Math.round(box.width) + ' is narrower than its value ' + Math.round(numBox.width));
	// What the chip draws: the value, and the word. Neither may run under an action.
	const ink = [num, chip.querySelector('.word')].filter(Boolean).map(el => {
		const range = document.createRange(); range.selectNodeContents(el);
		return { name: el.className, rect: range.getBoundingClientRect(), clip: el.getBoundingClientRect() };
	});
	for (const { name, rect, clip } of ink) {
		// text drawn outside the chip's own box (an ellipsized word is cut at its box, not here)
		const visibleRight = Math.min(rect.right, clip.right, box.right);
		for (const el of actions) {
			const a = el.getBoundingClientRect();
			if (visibleRight > a.left + 0.5 && rect.left < a.right && rect.bottom > a.top && rect.top < a.bottom) out.push(name + ' runs under ' + el.textContent.trim());
		}
		if (name === 'num' && rect.right > box.right + 1) out.push('value spills past the chip: ' + Math.round(rect.right) + ' > ' + Math.round(box.right));
	}
	return out.join('; ');
})()`
