package web

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The architecture assistant panel, against a fake of "Contract: Harness
// server ↔ Web UI — architecture assistant turns". The real claude is not in
// these tests.

const (
	asstOpen    = `[data-assistant-open]`
	asstPanel   = `[data-assistant-panel]`
	asstInput   = `[data-assistant-input]`
	asstSend    = `[data-assistant-send]`
	asstStop    = `[data-assistant-stop]`
	asstMessage = `[data-assistant-message]`
)

// asstShown is true when the panel is on screen.
const asstShown = `!!document.querySelector('[data-assistant-panel]')?.checkVisibility()`

// asstMessages are the conversation's messages as "who: text".
const asstMessages = `[...document.querySelectorAll('[data-assistant-message]')].map(m => m.dataset.who + ': ' + m.textContent.replace(/\s+/g, ' ').trim())`

// asstActivity are the progress lines of the running or last turn.
const asstActivity = `[...document.querySelectorAll('[data-assistant-activity]')].map(m => m.textContent.replace(/\s+/g, ' ').trim())`

// asstType puts text in the message box the way typing does, so the page's own listeners see it.
func asstType(text string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(() => {
		const el = document.querySelector('[data-assistant-input]');
		el.focus();
		el.value = %q;
		el.dispatchEvent(new Event('input', { bubbles: true }));
		return true;
	})()`, text), nil)
}

// asstKey sends a keydown to the message box, as the keyboard does.
func asstKey(selector, key string, shift bool) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		el.focus();
		return el.dispatchEvent(new KeyboardEvent('keydown', { key: %q, shiftKey: %t, bubbles: true, cancelable: true }));
	})()`, selector, key, shift), nil)
}

// asstLastSaid is true when the newest assistant message contains text.
func asstLastSaid(text string) string {
	return fmt.Sprintf(`[...document.querySelectorAll('[data-assistant-message][data-who=assistant]')].at(-1)?.textContent.includes(%q) === true`, text)
}

func asstOpenPanel() []chromedp.Action {
	return []chromedp.Action{
		poll(`!!document.querySelector('[data-assistant-open]') && !document.querySelector('[data-assistant-open]').disabled`),
		chromedp.Click(asstOpen, chromedp.ByQuery),
		poll(asstShown),
	}
}

func asstReady(url string) []chromedp.Action {
	return []chromedp.Action{
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url),
		poll(`document.querySelectorAll('[role=tab]').length > 0 && !!document.querySelector('#architecture-pattern')`),
	}
}

// Open, send, stream: the answer arrives piece by piece with a line for each
// file read, the final text replaces the pieces, and the draft goes with the turn.
func TestAssistantTalksAndStreamsInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	hold := make(chan struct{})
	fake.script(func(turn *assistantTurn) {
		turn.Event("activity", map[string]any{"tool": "Read", "target": "internal/domain/zone.go"})
		turn.Event("activity", map[string]any{"tool": "Grep", "target": "ports"})
		turn.Event("delta", map[string]any{"text": "The domain "})
		<-hold
		turn.Event("delta", map[string]any{"text": "imports nothing."})
		turn.Event("result", map[string]any{"text": "The domain imports nothing, so it is a pure core.", "proposal": nil})
	}, answer("Two layers fit."))

	var msgs, activity []string
	var focused bool
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t,
		chromedp.SetValue(`#architecture-pattern`, "hexagonal", chromedp.ByQuery), // an unsaved edit the turn must carry
	)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		chromedp.Evaluate(`document.activeElement === document.querySelector('[data-assistant-input]')`, &focused),
		asstType("What layers fit?"),
		asstKey(asstInput, "Enter", true), // Shift+Enter is a new line, not a send
	)
	if !focused {
		t.Error("the message box does not have focus when the panel opens")
	}
	if n := len(fake.turns()); n != 0 {
		t.Fatalf("Shift+Enter sent a turn (%d requests)", n)
	}
	rig.run(t,
		asstKey(asstInput, "Enter", false),
		poll(`document.querySelectorAll('[data-assistant-activity]').length === 2`),
		poll(`document.querySelector('[data-assistant-message][data-who=assistant]')?.textContent.includes('The domain')`),
		chromedp.Evaluate(asstActivity, &activity),
		poll(`!!document.querySelector('[data-assistant-stop]')?.checkVisibility()`),
	)
	if activity[0] != "Reading internal/domain/zone.go" || !strings.HasPrefix(activity[1], "Searching") {
		t.Errorf("activity lines = %q", activity)
	}
	close(hold)
	rig.run(t,
		poll(`document.querySelector('[data-assistant-message][data-who=assistant]')?.textContent.includes('so it is a pure core')`),
		poll(`!document.querySelector('[data-assistant-stop]')?.checkVisibility()`),
		chromedp.Evaluate(asstMessages, &msgs),
	)
	if len(msgs) != 2 || msgs[0] != "person: What layers fit?" || msgs[1] != "assistant: The domain imports nothing, so it is a pure core." {
		t.Errorf("messages = %q", msgs)
	}

	first := fake.turns()[0]
	if first["repository_id"] != "r1" || first["message"] != "What layers fit?" {
		t.Errorf("first turn = %v", first)
	}
	if _, has := first["conversation_id"]; has && first["conversation_id"] != "" {
		t.Errorf("first turn carries a conversation id: %v", first["conversation_id"])
	}
	draft, _ := first["draft"].(map[string]any)
	if draft["pattern"] != "hexagonal" {
		t.Errorf("the draft sent lacks the person's unsaved edit: %v", draft["pattern"])
	}
	if first["draft_base_version"] != float64(1) {
		t.Errorf("draft_base_version = %v", first["draft_base_version"])
	}
	if _, has := first["rule_event_id"]; has {
		t.Errorf("a plain message carries a rule event id")
	}

	// The panel stays while the tabs change, and a second turn resumes the conversation.
	rig.run(t,
		chromedp.Click(`#architecture-tab-layers`, chromedp.ByQuery),
		poll(`document.querySelector('#architecture-panel-layers').checkVisibility()`),
		chromedp.Evaluate(asstShown, nil),
		asstType("Go on."),
		chromedp.Click(asstSend, chromedp.ByQuery),
		poll(`document.querySelectorAll('[data-assistant-message][data-who=assistant]').length === 2`),
	)
	var shown bool
	rig.run(t, chromedp.Evaluate(asstShown, &shown))
	if !shown {
		t.Error("the panel closed when the tab changed")
	}
	second := fake.turns()[1]
	if second["conversation_id"] != "c1" {
		t.Errorf("second turn conversation_id = %v, want c1", second["conversation_id"])
	}
}

// Stop ends the turn at once, keeps the conversation, and the person can send again.
func TestAssistantStopEndsTheTurnInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	fake.script(func(turn *assistantTurn) {
		turn.Event("delta", map[string]any{"text": "Looking "})
		turn.Hold(10 * time.Second)
	}, answer("Back again."))

	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("Take your time."),
		chromedp.Click(asstSend, chromedp.ByQuery),
		poll(asstLastSaid("Looking")),
		chromedp.Click(asstStop, chromedp.ByQuery),
		poll(`!document.querySelector('[data-assistant-stop]')?.checkVisibility()`),
	)
	waitFor(t, "the aborted request", func() bool { return fake.abortedTurns() == 1 })
	rig.run(t,
		asstType("Again."),
		chromedp.Click(asstSend, chromedp.ByQuery),
		poll(asstLastSaid("Back again.")),
	)
	if got := fake.turns()[1]["conversation_id"]; got != "c1" {
		t.Errorf("after Stop the next turn conversation_id = %v, want c1", got)
	}
}

// Escape closes the panel and ends the conversation; so does leaving the page.
func TestAssistantCloseEndsTheConversationInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	fake.script(answer("Hello."), answer("Hello again."))

	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("Hi"),
		asstKey(asstInput, "Enter", false),
		poll(`document.querySelectorAll('[data-assistant-message]').length === 2`),
		asstKey(asstInput, "Escape", false),
		poll(`!`+asstShown),
	)
	waitFor(t, "the end request", func() bool { return len(fake.endedIDs()) == 1 })
	if got := fake.endedIDs()[0]; got != "c1" {
		t.Errorf("ended conversation = %q, want c1", got)
	}

	// Reopened, it is a new conversation with nothing in it.
	var msgs []string
	rig.run(t, asstOpenPanel()...)
	rig.run(t, chromedp.Evaluate(asstMessages, &msgs))
	if len(msgs) != 0 {
		t.Errorf("a reopened panel still has messages: %q", msgs)
	}
	rig.run(t,
		asstType("Hi"),
		asstKey(asstInput, "Enter", false),
		poll(`document.querySelectorAll('[data-assistant-message]').length === 2`),
		chromedp.Navigate(rig.srv.URL+"/projects/p1/repositories"),
	)
	waitFor(t, "the end request on leaving the page", func() bool { return len(fake.endedIDs()) == 2 })
	if got := fake.endedIDs()[1]; got != "c2" {
		t.Errorf("ended on unload = %q, want c2", got)
	}
}

// With no assistant the button is disabled, says why and links to the fix.
func TestAssistantUnavailableShowsTheReasonInTheBrowser(t *testing.T) {
	rig, _ := newAssistantRig(t, false, "Claude is not signed in.", "/agent-isolation#credential")
	rig.api.seed(savedRecord())
	var disabled bool
	var reason, href string
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t,
		poll(`!!document.querySelector('[data-assistant-open]')`),
		chromedp.Evaluate(`document.querySelector('[data-assistant-open]').disabled`, &disabled),
		poll(`!!document.querySelector('[data-assistant-unavailable]')`),
		chromedp.Evaluate(`document.querySelector('[data-assistant-unavailable]').textContent.replace(/\s+/g, ' ').trim()`, &reason),
		chromedp.Evaluate(`document.querySelector('[data-assistant-unavailable] a').getAttribute('href')`, &href),
	)
	if !disabled {
		t.Error("the Assistant button is enabled although the assistant is unavailable")
	}
	if !strings.Contains(reason, "Claude is not signed in.") {
		t.Errorf("reason = %q", reason)
	}
	if href != "/agent-isolation#credential" {
		t.Errorf("fix link = %q", href)
	}
}

// A conversation the server no longer knows is told, and a new one is offered.
func TestAssistantLostConversationOffersANewOneInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	fake.script(answer("One."), func(turn *assistantTurn) {
		turn.Fail(404, "CONVERSATION_NOT_FOUND", "unknown conversation", nil)
	}, answer("Fresh start."))

	var lost string
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("First"),
		asstKey(asstInput, "Enter", false),
		poll(`document.querySelectorAll('[data-assistant-message]').length === 2`),
		asstType("Second"),
		asstKey(asstInput, "Enter", false),
		poll(`!!document.querySelector('[data-assistant-lost]')`),
		chromedp.Evaluate(`document.querySelector('[data-assistant-lost]').textContent.replace(/\s+/g, ' ').trim()`, &lost),
	)
	if !strings.Contains(strings.ToLower(lost), "lost") {
		t.Errorf("lost notice = %q", lost)
	}
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('[data-assistant-new]').click()`, nil),
		poll(`!document.querySelector('[data-assistant-lost]')`),
		asstType("Third"),
		asstKey(asstInput, "Enter", false),
		poll(asstLastSaid("Fresh start.")),
	)
	third := fake.turns()[2]
	if id, _ := third["conversation_id"].(string); id != "" {
		t.Errorf("a new conversation reused id %q", id)
	}
}

// A failed turn is shown and the conversation stays usable.
func TestAssistantFailedTurnKeepsTheConversationInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	fake.script(func(turn *assistantTurn) {
		turn.Event("delta", map[string]any{"text": "Half "})
		turn.Event("error", map[string]any{"code": "ARCHITECTURE_ASSISTANT_FAILED", "message": "The assistant stopped before it finished."})
	}, answer("Second try."))

	var errText string
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("First"),
		asstKey(asstInput, "Enter", false),
		poll(`!!document.querySelector('[data-assistant-error]')`),
		chromedp.Evaluate(`document.querySelector('[data-assistant-error]').textContent.replace(/\s+/g, ' ').trim()`, &errText),
	)
	if !strings.Contains(errText, "The assistant stopped before it finished.") {
		t.Errorf("error = %q", errText)
	}
	rig.run(t,
		asstType("Again"),
		asstKey(asstInput, "Enter", false),
		poll(asstLastSaid("Second try.")),
	)
	if got := fake.turns()[1]["conversation_id"]; got != "c1" {
		t.Errorf("after a failure the next turn conversation_id = %v, want c1", got)
	}
}
