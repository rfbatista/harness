package web

import (
	"context"
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
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="/design-system/css/index.css"></head>` +
			`<body><div class="session-head"><p class="meta" id="root" style="display:contents"></p></div><button id="elsewhere" style="position:fixed;inset-block-end:1rem;inset-inline-end:1rem">elsewhere</button><script src="/harness.js"></script></body></html>`))
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
