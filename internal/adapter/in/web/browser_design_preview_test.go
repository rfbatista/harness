package web

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// geometry reports, for the preview under root, the stage's content box, the
// iframe's border box (zero when there is none) and the media's border box
// (zero when there is none), all in CSS pixels.
const previewGeometry = `(root => {
  const stage = root.querySelector('.preview > .stage');
  const cs = getComputedStyle(stage);
  const sr = stage.getBoundingClientRect();
  const box = el => { const r = el?.getBoundingClientRect(); return r ? {x: r.x, y: r.y, w: r.width, h: r.height} : {x: 0, y: 0, w: 0, h: 0}; };
  const pl = parseFloat(cs.paddingLeft), pt = parseFloat(cs.paddingTop);
  return {
    stage: {x: sr.x + pl, y: sr.y + pt, w: stage.clientWidth - pl - parseFloat(cs.paddingRight), h: stage.clientHeight - pt - parseFloat(cs.paddingBottom)},
    iframe: box(stage.querySelector('iframe')),
    media: box(stage.querySelector('img')),
  };
})`

type rect struct{ X, Y, W, H float64 }

type previewBoxes struct{ Stage, Iframe, Media rect }

func near(a, b float64) bool { return a-b < 1 && b-a < 1 }

// TestThePreviewFillsItsPaneInTheBrowser: a page opened on any of the three
// Design views is as wide and as tall as the stage it sits in, at a wide
// viewport, and an image stays centred across and no larger than the stage.
// Contract: design system ↔ web UI — the preview fills its pane.
func TestThePreviewFillsItsPaneInTheBrowser(t *testing.T) {
	now := time.Now()

	// The session's tab and the task's page list what the task made; the
	// project's page lists only what was moved to the project.
	views := []struct{ name, path, root, scope string }{
		{"task Design assets page", "/projects/p1/tasks/t1/design", `document.querySelector('main')`, "task"},
		{"project Design assets page", "/projects/p1/design", `document.querySelector('main')`, "project"},
		{"session Design tab", "/projects/p1/tasks/t1", designPanelSel, "task"},
	}
	for _, v := range views {
		t.Run(v.name, func(t *testing.T) {
			api := &scopedArtifactsAPI{feed: &sseFeed{}, list: []map[string]any{ // newest first
				scopedDTO("a1", "page", "Hero", "design/hero.html", v.scope, now),
				scopedDTO("a-pic", "image", "Photo", "design/photo.png", v.scope, now.Add(-time.Minute)),
			}}
			srv := designServer(t, api)
			ctx, errs := browser(t)
			actions := []chromedp.Action{
				chromedp.EmulateViewport(1600, 1000),
				chromedp.Navigate(srv.URL + v.path),
			}
			if v.root == designPanelSel {
				actions = append(actions,
					chromedp.Poll(`!!document.querySelector('[role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
					clickButton(`[role=tablist]`, "Design"))
			}
			var page, image previewBoxes
			actions = append(actions,
				chromedp.Poll(v.root+`?.querySelector('.preview iframe')?.getAttribute('src')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Poll(`(() => { const r = `+v.root+`.querySelector('.preview iframe').getBoundingClientRect(); return r.width > 0 })()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Evaluate(previewGeometry+`(`+v.root+`)`, &page),
				chromedp.Evaluate(`(`+cardTitleIn(v.root, "Photo")+`).click(); true`, nil),
				chromedp.Poll(v.root+`.querySelector('.preview img')?.naturalWidth > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Evaluate(previewGeometry+`(`+v.root+`)`, &image),
			)
			if err := chromedp.Run(ctx, actions...); err != nil {
				t.Fatalf("%v\nJS errors: %v", err, errs.all())
			}
			if !near(page.Iframe.W, page.Stage.W) || !near(page.Iframe.H, page.Stage.H) {
				t.Errorf("page iframe is %.0f × %.0f, stage is %.0f × %.0f: want the iframe to fill it", page.Iframe.W, page.Iframe.H, page.Stage.W, page.Stage.H)
			}
			m, s := image.Media, image.Stage
			if m.W > s.W+1 || m.H > s.H+1 {
				t.Errorf("image is %.0f × %.0f, larger than the stage %.0f × %.0f", m.W, m.H, s.W, s.H)
			}
			// Horizontal only: the frame wrapper is a full-height column
			// (compositions/frame.css), so media sits at the top of the stage.
			if dx := (m.X + m.W/2) - (s.X + s.W/2); !near(dx, 0) {
				t.Errorf("image centre is off the stage centre by %.1f px (image %+v, stage %+v)", dx, m, s)
			}
		})
	}
}

func cardTitleIn(root, title string) string {
	return fmt.Sprintf(`[...%s.querySelectorAll('.artifact-card')].find(c => c.querySelector('.title').textContent === %q)`, root, title)
}
