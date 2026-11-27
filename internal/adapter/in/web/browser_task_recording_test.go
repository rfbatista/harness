package web

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// fakeTaskRecordings answers a task's recording as the bench does: the task's
// own flag, else the project default (off here).
type fakeTaskRecordings struct{ explicit *bool }

func (f fakeTaskRecordings) TaskRecording(_ context.Context, _, taskID, _ string) (domain.TaskTrajectoryRecording, error) {
	rec := domain.TaskTrajectoryRecording{TaskID: taskID, Source: "project", Default: false, DefaultSource: "project"}
	if f.explicit != nil {
		rec.Enabled, rec.Explicit, rec.Source = *f.explicit, f.explicit, "task"
	}
	return rec, nil
}

// The Edit form's fourth section is the task's trajectory recording in the
// inherit / on / off shape, with the resolution line following the choice.
func TestTheEditFormShowsTheTrajectoryRecordingWithItsResolutionLine(t *testing.T) {
	deps := taskPageDeps()
	deps.TaskRecordings = fakeTaskRecordings{}
	url, _ := pageServer(t, deps)
	ctx, errs := browser(t)

	const section = `document.querySelector('#task-note .task-recording')`
	var heading, value, line, afterChoice string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('task-edit-button').click()`, nil),
		chromedp.Poll(`!!`+section+` && `+section+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(section+`.querySelector('h4').textContent`, &heading),
		chromedp.Evaluate(section+`.querySelector('select').value`, &value),
		chromedp.Evaluate(section+`.querySelector('[data-resolved]').textContent`, &line),
		chromedp.Evaluate(`(() => { const s = `+section+`.querySelector('select'); s.value = 'on'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
		chromedp.Poll(`(`+section+`.querySelector('[data-resolved]').textContent) === 'on, from this task; project default off'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(section+`.querySelector('[data-resolved]').textContent`, &afterChoice),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if heading != "Trajectory recording" || value != "inherit" || line != "off, from the project default" {
		t.Errorf("section = %q, choice %q, line %q", heading, value, line)
	}
	if afterChoice != "on, from this task; project default off" {
		t.Errorf("line after choosing on = %q", afterChoice)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// With no recordings served the Edit form has no such section.
func TestTheEditFormHasNoTrajectoryRecordingWhenNoneIsServed(t *testing.T) {
	url, _ := pageServer(t, taskPageDeps())
	ctx, errs := browser(t)
	var present bool
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('task-edit-button').click()`, nil),
		chromedp.Poll(`document.getElementById('task-note').matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`(() => { const s = document.querySelector('#task-note .task-recording'); return !!s && s.checkVisibility(); })()`, &present),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if present {
		t.Error("a Trajectory recording section without recordings served")
	}
}
