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

// proposalOf is a proposal as the contract's result event carries it.
func proposalOf(id, summary string, changes ...map[string]any) map[string]any {
	return map[string]any{"id": id, "summary": summary, "valid": true, "changes": changes, "errors": []any{}}
}

// change is one proposal change; base is the target's value in the draft the turn was sent with.
func change(target map[string]any, op string, value, base any, reason string) map[string]any {
	c := map[string]any{"target": target, "op": op, "base": base, "reason": reason}
	if value != nil {
		c["value"] = value
	}
	return c
}

// proposing is the script of a turn that answers with a proposal.
func proposing(text string, proposal map[string]any) func(t *assistantTurn) {
	return func(t *assistantTurn) {
		t.Event("delta", map[string]any{"text": text})
		t.Event("result", map[string]any{"text": text, "proposal": proposal})
	}
}

// portsProposal adds a ports layer and sets the pattern: two changes on two tabs.
func portsProposal(id string) map[string]any {
	return proposalOf(id, "Add the ports layer.",
		change(map[string]any{"kind": "layer", "name": "ports"}, "add",
			map[string]any{"name": "ports", "match": map[string]any{"folder": "internal/ports"}, "access": map[string]any{"default": "read"}}, nil,
			"internal/ports holds the interfaces the adapters implement."),
		change(map[string]any{"kind": "pattern"}, "change", "hexagonal", nil, "The code follows ports and adapters."),
	)
}

const (
	asstCard     = `[data-assistant-proposal]`
	cardState    = `document.querySelector('[data-assistant-proposal]')?.dataset.state`
	layersCount  = `document.querySelector('#architecture-tab-layers [data-tab-count]').textContent.trim()`
	barSummaryJS = `document.querySelector('[data-save-bar] .summary')?.textContent.replace(/\s+/g, ' ').trim() ?? ''`
)

// A proposal is a card with its summary, its changes grouped by tab with their reasons, and the layer
// preview for the draft with it applied. Accepting puts it in the unsaved draft and stores nothing.
func TestAssistantProposalCardAcceptsIntoTheDraftInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	fake.script(proposing("Here is a proposal.", portsProposal("p1")))

	var card, bar, count string
	var groups []string
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("Add a ports layer"),
		asstKey(asstInput, "Enter", false),
		poll(`!!document.querySelector('[data-assistant-proposal] [data-proposal-preview]')?.textContent.includes('ports 1 file')`),
		chromedp.Evaluate(`document.querySelector('[data-assistant-proposal]').textContent.replace(/\s+/g, ' ')`, &card),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-proposal-group]')].map(g => g.dataset.group)`, &groups),
	)
	for _, want := range []string{
		"Add the ports layer.", "Layer ports", "folder internal/ports", "internal/ports holds the interfaces the adapters implement.",
		"The code follows ports and adapters.", "Add", "Change", "domain", "adapters", "ports 1 file", "No overlaps",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("proposal card lacks %q: %q", want, card)
		}
	}
	if strings.Join(groups, ",") != "pattern,layers" {
		t.Errorf("groups = %v, want pattern,layers", groups)
	}
	// The preview was asked for the draft with the proposal applied, and nothing is in the draft yet.
	rig.run(t, chromedp.Evaluate(layersCount, &count), chromedp.Evaluate(barVisible(), nil))
	if count != "2" {
		t.Fatalf("the layers tab counted %s before accept", count)
	}

	rig.run(t,
		chromedp.Click(`[data-proposal-accept]`, chromedp.ByQuery),
		poll(`document.querySelector('#architecture-tab-layers [data-tab-count]').textContent.trim() === '3'`),
		poll(barVisible()),
		chromedp.Evaluate(barSummaryJS, &bar),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &count),
	)
	if count != "hexagonal" {
		t.Errorf("pattern after accept = %q", count)
	}
	if !strings.Contains(bar, "2 changes") {
		t.Errorf("save bar = %q", bar)
	}
	var state string
	rig.run(t, chromedp.Evaluate(cardState, &state))
	if state != "accepted" {
		t.Errorf("card state = %q", state)
	}
	for _, c := range rig.api.called() {
		if c == "save_repository_architecture" {
			t.Fatalf("accepting stored something: %v", rig.api.called())
		}
	}

	// Review diff shows the accepted changes; Save stores them as usual.
	rig.run(t,
		clickButton(`[data-save-bar]`, "Save to database"),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	if got := len(rig.api.saved); got != 1 {
		t.Fatalf("saves = %d, want 1", got)
	}
}

// Discarding a proposal leaves the draft as it was.
func TestAssistantProposalDiscardLeavesTheDraftInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	fake.script(proposing("Here is a proposal.", portsProposal("p1")))

	var count string
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("Add a ports layer"),
		asstKey(asstInput, "Enter", false),
		poll(`!!document.querySelector('[data-proposal-discard]')`),
		chromedp.Click(`[data-proposal-discard]`, chromedp.ByQuery),
		poll(cardState+` === 'discarded'`),
		chromedp.Evaluate(layersCount, &count),
	)
	if count != "2" {
		t.Errorf("layers after discard = %s", count)
	}
	var shown bool
	rig.run(t,
		chromedp.Evaluate(`!!document.querySelector('[data-proposal-accept]')`, &shown),
	)
	if shown {
		t.Error("a discarded card still offers Accept")
	}
	rig.run(t, chromedp.Evaluate(`document.querySelector('[data-save-bar]').checkVisibility()`, &shown))
	if shown {
		t.Error("the save bar shows after a discard")
	}
}
