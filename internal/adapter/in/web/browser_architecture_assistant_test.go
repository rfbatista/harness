package web

import (
	"context"
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

// asstClick clicks the first visible, enabled element matching selector, waiting for one: a click by
// coordinates can miss a button in the panel's log, which moves as cards change state.
func asstClick(selector string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := fmt.Sprintf(`(() => {
			const b = [...document.querySelectorAll(%q)].find((x) => x.checkVisibility() && !x.disabled);
			if (!b) return false;
			b.click();
			return true;
		})()`, selector)
		deadline := time.Now().Add(8 * time.Second)
		for {
			var ok bool
			if err := chromedp.Evaluate(js, &ok).Do(ctx); err != nil {
				return err
			}
			if ok {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("no enabled %s to click", selector)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
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
		asstClick(`#architecture-tab-layers`),
		poll(`document.querySelector('#architecture-panel-layers').checkVisibility()`),
		chromedp.Evaluate(asstShown, nil),
		asstType("Go on."),
		asstClick(asstSend),
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
		asstClick(asstSend),
		poll(asstLastSaid("Looking")),
		asstClick(asstStop),
		poll(`!document.querySelector('[data-assistant-stop]')?.checkVisibility()`),
	)
	waitFor(t, "the aborted request", func() bool { return fake.abortedTurns() == 1 })
	rig.run(t,
		asstType("Again."),
		asstClick(asstSend),
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
		change(map[string]any{"kind": "rule", "id": "ports-only"}, "add",
			map[string]any{"id": "ports-only", "kind": "edit_outside_layer", "mode": "blocking", "message": "Keep ports pure."}, nil, "Nothing outside ports may edit it."),
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
	if strings.Join(groups, ",") != "pattern,hooks,layers" {
		t.Errorf("groups = %v, want pattern,hooks,layers", groups)
	}
	var blocking int
	var badge string
	rig.run(t,
		chromedp.Evaluate(`document.querySelectorAll('[data-proposal-blocking]').length`, &blocking),
		chromedp.Evaluate(`document.querySelector('[data-proposal-blocking]')?.closest('[data-proposal-change]')?.textContent.replace(/\s+/g, ' ') ?? ''`, &badge),
	)
	if blocking != 1 || !strings.Contains(badge, "ports-only") {
		t.Errorf("blocking badges = %d on %q, want one on the ports-only rule", blocking, badge)
	}
	// The preview was asked for the draft with the proposal applied, and nothing is in the draft yet.
	rig.run(t, chromedp.Evaluate(layersCount, &count), chromedp.Evaluate(barVisible(), nil))
	if count != "2" {
		t.Fatalf("the layers tab counted %s before accept", count)
	}

	rig.run(t,
		asstClick(`[data-proposal-accept]`),
		poll(`document.querySelector('#architecture-tab-layers [data-tab-count]').textContent.trim() === '3'`),
		poll(barVisible()),
		chromedp.Evaluate(barSummaryJS, &bar),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &count),
	)
	if count != "hexagonal" {
		t.Errorf("pattern after accept = %q", count)
	}
	if !strings.Contains(bar, "3 changes") {
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
		asstClick(`[data-proposal-discard]`),
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

// conflictCard sends a turn whose proposal sets the pattern and adds the ports layer, then has the person
// edit the pattern before accepting: the pattern change now collides with an unsaved edit.
func conflictCard(t *testing.T) (*archRig, *fakeAssistant) {
	t.Helper()
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	fake.script(proposing("Here is a proposal.", portsProposal("p1")))
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("Add a ports layer"),
		asstKey(asstInput, "Enter", false),
		poll(`!!document.querySelector('[data-proposal-accept]')`),
		setField(patternField, "mine", "input"),
		asstClick(`[data-proposal-accept]`),
		poll(cardState+` === 'conflicts'`),
	)
	return rig, fake
}

// A change that touches what the person edited is shown with both values, and nothing lands until the
// person has picked one for it. Keeping the person's value leaves it alone; the rest still lands.
func TestAssistantConflictKeepsThePersonsValueInTheBrowser(t *testing.T) {
	rig, _ := conflictCard(t)
	var text, count, pattern string
	var disabled bool
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('[data-proposal-conflict]').textContent.replace(/\s+/g, ' ')`, &text),
		chromedp.Evaluate(`document.querySelector('[data-conflict-apply]').disabled`, &disabled),
		chromedp.Evaluate(layersCount, &count),
	)
	for _, want := range []string{"Pattern", "mine", "hexagonal"} {
		if !strings.Contains(text, want) {
			t.Errorf("conflict lacks %q: %q", want, text)
		}
	}
	if !disabled {
		t.Error("Apply is enabled before every conflict has a pick")
	}
	if count != "2" {
		t.Fatalf("a proposal landed in part before the pick: layers = %s", count)
	}
	rig.run(t,
		asstClick(`[data-conflict-keep]`),
		poll(`!document.querySelector('[data-conflict-apply]').disabled`),
		asstClick(`[data-conflict-apply]`),
		poll(cardState+` === 'accepted'`),
		chromedp.Evaluate(layersCount, &count),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &pattern),
	)
	if count != "3" {
		t.Errorf("layers after apply = %s, want 3", count)
	}
	if pattern != "mine" {
		t.Errorf("pattern = %q, want the person's value", pattern)
	}
}

// Taking the proposal's value for the conflict replaces the person's edit.
func TestAssistantConflictTakesTheProposalsValueInTheBrowser(t *testing.T) {
	rig, _ := conflictCard(t)
	var pattern string
	rig.run(t,
		asstClick(`[data-conflict-take]`),
		asstClick(`[data-conflict-apply]`),
		poll(cardState+` === 'accepted'`),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &pattern),
	)
	if pattern != "hexagonal" {
		t.Errorf("pattern = %q, want the proposal's value", pattern)
	}
}

// Cancelling the conflict view takes the card back to open with the draft untouched.
func TestAssistantConflictCancelLeavesTheDraftInTheBrowser(t *testing.T) {
	rig, _ := conflictCard(t)
	var count string
	rig.run(t,
		asstClick(`[data-conflict-cancel]`),
		poll(cardState+` === 'open'`),
		chromedp.Evaluate(layersCount, &count),
	)
	if count != "2" {
		t.Errorf("layers = %s after cancel", count)
	}
}

// A reply that gets a revised proposal marks the earlier card as superseded: it offers no Accept.
func TestAssistantRefineSupersedesTheEarlierCardInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	revised := proposalOf("p2", "Ports layer, blocking rule.",
		change(map[string]any{"kind": "layer", "name": "ports"}, "add",
			map[string]any{"name": "ports", "match": map[string]any{"folder": "internal/ports"}, "access": map[string]any{"default": "read"}}, nil, "Interfaces."))
	fake.script(proposing("First.", portsProposal("p1")), proposing("Revised.", revised))

	var states string
	var accepts int
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("Add a ports layer"),
		asstKey(asstInput, "Enter", false),
		poll(`document.querySelectorAll('[data-assistant-proposal]').length === 1 && !document.querySelector('[data-assistant-stop]')`),
		asstType("Leave out the pattern."),
		asstKey(asstInput, "Enter", false),
		poll(`document.querySelectorAll('[data-assistant-proposal]').length === 2`),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-assistant-proposal]')].map(c => c.dataset.state).join(',')`, &states),
		chromedp.Evaluate(`document.querySelectorAll('[data-proposal-accept]').length`, &accepts),
	)
	if states != "superseded,open" {
		t.Errorf("card states = %q, want superseded,open", states)
	}
	if accepts != 1 {
		t.Errorf("%d Accept buttons, want only the newest card's", accepts)
	}
}

// A proposal the server marked invalid shows its errors as the save bar words them, and offers no Accept.
func TestAssistantInvalidProposalShowsItsErrorsInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	bad := proposalOf("p1", "A layer that does not compile.",
		change(map[string]any{"kind": "layer", "name": "ports"}, "add",
			map[string]any{"name": "ports", "match": map[string]any{"regex": "(^internal"}}, nil, "Ports."))
	bad["valid"] = false
	bad["errors"] = []any{map[string]any{"path": "/layers/2/match/regex", "code": "INVALID_REGEX", "message": "does not compile"}}
	fake.script(proposing("Here.", bad))

	var errs string
	var accept bool
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("Add ports"),
		asstKey(asstInput, "Enter", false),
		poll(`!!document.querySelector('[data-proposal-error]')`),
		chromedp.Evaluate(`document.querySelector('[data-proposal-error]').textContent.replace(/\s+/g, ' ').trim()`, &errs),
		chromedp.Evaluate(`!!document.querySelector('[data-proposal-accept]')`, &accept),
	)
	if errs != "Layers › Layers › 3 › match › regex: does not compile" {
		t.Errorf("error line = %q", errs)
	}
	if accept {
		t.Error("an invalid proposal offers Accept")
	}
	var preview bool
	rig.run(t, chromedp.Evaluate(`!!document.querySelector('[data-proposal-preview]')`, &preview))
	if preview {
		t.Error("the card shows the layer preview of an invalid proposal")
	}
}

// "Ask the assistant" on a rule event opens the panel with the event quoted; the turn it sends carries the
// event's id, and a later plain message does not.
func TestAssistantAskFromARuleEventInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	rig.api.events = []map[string]any{
		ruleEvent("e2", "go-developer", "stay-in-your-layer", "blocked", "internal/domain/x.go", "domain"),
		ruleEvent("e1", "plain", "archtest", "warned", "cmd/server/main.go", ""),
	}
	fake.script(answer("It fired because go-developer may not change domain."), answer("Anything else?"))

	var quote, input string
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t,
		poll(`document.querySelectorAll('[data-rule-event]').length === 2`),
		asstClick(`[data-rule-event] [data-rule-event-ask]`),
		poll(asstShown),
		poll(`!!document.querySelector('[data-assistant-quote]')`),
		chromedp.Evaluate(`document.querySelector('[data-assistant-quote]').textContent.replace(/\s+/g, ' ').trim()`, &quote),
		chromedp.Evaluate(`document.querySelector('[data-assistant-input]').value`, &input),
	)
	for _, want := range []string{"go-developer", "stay-in-your-layer", "internal/domain/x.go"} {
		if !strings.Contains(quote, want) {
			t.Errorf("quote lacks %q: %q", want, quote)
		}
	}
	if strings.TrimSpace(input) == "" {
		t.Error("the message box is empty: the person has nothing to send")
	}
	rig.run(t,
		asstKey(asstInput, "Enter", false),
		poll(asstLastSaid("may not change domain")),
		poll(`!document.querySelector('[data-assistant-quote]')`),
		asstType("Thanks."),
		asstKey(asstInput, "Enter", false),
		poll(asstLastSaid("Anything else?")),
	)
	turns := fake.turns()
	if turns[0]["rule_event_id"] != "e2" {
		t.Errorf("first turn rule_event_id = %v, want e2", turns[0]["rule_event_id"])
	}
	if _, has := turns[1]["rule_event_id"]; has {
		t.Errorf("a later plain message resent the rule event id: %v", turns[1]["rule_event_id"])
	}
}

// With no assistant the events list offers no "Ask the assistant".
func TestAssistantAskIsNotOfferedWithoutAnAssistantInTheBrowser(t *testing.T) {
	rig, _ := newAssistantRig(t, false, "Claude is not signed in.", "/agent-isolation#credential")
	rig.api.seed(savedRecord())
	rig.api.events = []map[string]any{ruleEvent("e1", "plain", "archtest", "warned", "cmd/server/main.go", "")}
	var asks int
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t,
		poll(`document.querySelectorAll('[data-rule-event]').length === 1`),
		chromedp.Evaluate(`document.querySelectorAll('[data-rule-event-ask]').length`, &asks),
	)
	if asks != 0 {
		t.Errorf("%d Ask the assistant buttons, want none", asks)
	}
}

// The open panel makes room for the page: after Accept, Review diff and Save are visible and nothing
// covers them, so the person can save without closing the panel.
func TestAssistantPanelPushesThePageInTheBrowser(t *testing.T) {
	rig, fake := newAssistantRig(t, true, "", "")
	rig.api.seed(savedRecord())
	fake.script(proposing("Here is a proposal.", portsProposal("p1")))

	var covered []string
	rig.run(t, asstReady(rig.url(""))...)
	rig.run(t, asstOpenPanel()...)
	rig.run(t,
		asstType("Add a ports layer"),
		asstKey(asstInput, "Enter", false),
		poll(`!!document.querySelector('[data-proposal-accept]')`),
		asstClick(`[data-proposal-accept]`),
		poll(cardState+` === 'accepted'`),
		poll(barVisible()),
		chromedp.Evaluate(`['Review diff', 'Save to database'].map((label) => {
			const b = [...document.querySelectorAll('[data-save-bar] button')].find((x) => x.textContent.trim() === label);
			const r = b.getBoundingClientRect();
			const top = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
			return b.contains(top) ? '' : label + ' is covered by ' + (top ? top.outerHTML.slice(0, 80) : 'nothing');
		}).filter(Boolean)`, &covered),
	)
	if len(covered) != 0 {
		t.Errorf("with the panel open: %v", covered)
	}
	// The page ends where the panel starts: the tab strip is not under it (it scrolls inside itself).
	var right float64
	var panelLeft float64
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('#architecture-tabs').getBoundingClientRect().right`, &right),
		chromedp.Evaluate(`document.querySelector('[data-assistant-panel]').getBoundingClientRect().left`, &panelLeft),
	)
	if right > panelLeft {
		t.Errorf("the tab strip reaches %vpx, under the panel that starts at %vpx", right, panelLeft)
	}
}
