package web

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func ruleEvent(id, agent, rule, outcome, path, layer string) map[string]any {
	mode := "blocking"
	if outcome == "warned" {
		mode = "warning"
	}
	return map[string]any{
		"id": id, "at": "2026-10-24T12:00:00Z", "session_id": "s-" + id, "task_id": "t-" + id,
		"agent": agent, "rule_id": rule, "kind": "edit_outside_layer", "at_point": "before_tool",
		"mode": mode, "outcome": outcome, "path": path, "layer": layer, "tool": "Edit", "detail": "",
	}
}

const eventRows = `[...document.querySelectorAll('[data-rule-event]')].map(e => e.textContent.replace(/\s+/g, ' ').trim())`

// The overview lists recent rule events with the outcome in words and a link to
// the session, and a new event on the project feed lands at the top.
func TestArchitectureOverviewListsRuleEventsLive(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	rig.api.events = []map[string]any{
		ruleEvent("e2", "go-developer", "stay-in-your-layer", "blocked", "internal/domain/x.go", "domain"),
		ruleEvent("e1", "plain", "archtest", "warned", "cmd/server/main.go", ""),
	}
	var rows []string
	var href string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[data-rule-event]').length === 2`),
		chromedp.Evaluate(eventRows, &rows),
		chromedp.Evaluate(`document.querySelector('[data-rule-event] a').getAttribute('href')`, &href),
	)
	first := rows[0]
	for _, want := range []string{"go-developer", "stay-in-your-layer", "Blocked", "✕", "internal/domain/x.go", "domain"} {
		if !strings.Contains(first, want) {
			t.Errorf("first event %q lacks %q", first, want)
		}
	}
	if !strings.Contains(rows[1], "Warned") || !strings.Contains(rows[1], "!") {
		t.Errorf("second event = %q", rows[1])
	}
	if href != "/projects/p1/tasks/t-e2?session=s-e2" {
		t.Errorf("session link = %q", href)
	}

	waitFor(t, "the page's feed", func() bool { return rig.feed.followers() >= 1 })
	ev := ruleEvent("e3", "devops", "stay-in-your-layer", "blocked", "README.md", "")
	rig.api.emit(map[string]any{"architecture_rule_event": map[string]any{"repository_id": "r1", "event": ev}})
	rig.api.emit(map[string]any{"architecture_rule_event": map[string]any{"repository_id": "other", "event": ruleEvent("e9", "x", "y", "blocked", "z", "")}})
	rig.run(t, poll(`document.querySelectorAll('[data-rule-event]').length === 3`))
	rig.run(t, chromedp.Evaluate(eventRows, &rows))
	if !strings.Contains(rows[0], "devops") {
		t.Fatalf("the new event is not on top: %q", rows)
	}
	for _, r := range rows {
		if strings.Contains(r, " z ") || strings.Contains(r, "e9") {
			t.Fatalf("another repository's event was shown: %q", rows)
		}
	}
}
