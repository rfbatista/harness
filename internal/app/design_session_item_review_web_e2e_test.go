//go:build !windows

package app

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The person decides a proposal item by item on the Proposals tab (wireframe
// proposal-review; ADR 0055). Screenshots go to ARCHITECT_SHOTS as
// impl-item-review-*.png.

// draftEdit changes one file of a design session's draft.
type draftEdit struct {
	path    string
	rewrite func(string) string
}

// proposedItems starts a design session on the page's design system, makes
// the edits in its draft and submits it.
func (s *designSessionPage) proposedItems(summary string, edits ...draftEdit) (sessionID string) {
	s.t.Helper()
	ctx := context.Background()
	sess, err := s.orch.StartDesignSession(ctx, s.ds, s.agent, "Rework the tokens.")
	if err != nil {
		s.t.Fatal(err)
	}
	view, err := s.cat.DesignSystems.DesignDraftFiles(ctx, sess.ID, s.ds)
	if err != nil || view == nil {
		s.t.Fatalf("draft = %v, %v", view, err)
	}
	files := map[string]string{}
	for _, f := range view.Files {
		files[f.Path] = string(f.Content)
	}
	for _, edit := range edits {
		from := edit.path
		if _, ok := files[from]; !ok {
			from = "components/button.html" // a new component starts as the button's page
		}
		files[edit.path] = edit.rewrite(files[from])
		if err := s.cat.DesignSystems.WriteDraftFile(ctx, sess.ID, s.ds, edit.path, []byte(files[edit.path])); err != nil {
			s.t.Fatal(err)
		}
	}
	if _, err := s.cat.DesignSystems.SubmitDesignProposal(ctx, sess.ID, summary); err != nil {
		s.t.Fatal(err)
	}
	return sess.ID
}

var (
	addInk = draftEdit{"tokens.json", func(t string) string {
		return strings.Replace(t, `"signal"`, `"ink": {"$type": "color", "$value": "#111111"}, "signal"`, 1)
	}}
	calmerSignal = draftEdit{"tokens.json", func(t string) string { return strings.Replace(t, "oklch(0.5 0.095 182)", "oklch(0.35 0.1 200)", 1) }}
	addChip      = draftEdit{"components/chip.html", func(button string) string {
		return strings.NewReplacer("Button", "Chip", "button", "chip", "colors-signal", "colors-ink").Replace(button)
	}}
)

// item is the selector of one item's panel and what is in it.
func item(id string, rest ...string) string {
	return `[data-proposal-panel][data-item-id="` + id + `"]` + strings.Join(rest, "")
}

// Some items approved, one rejected, applied: one version holds the approved
// ones and the proposal reads partly approved with each outcome.
func TestItemReviewInTheBrowserAppliesSomeItemsAndShowsPartlyApproved(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	// Three items: ink is added, the signal colour and the medium spacing change.
	s.proposedItems("Ink, calmer signal, wider spacing", addInk, calmerSignal, draftEdit{"tokens.json", func(t string) string { return strings.Replace(t, `"1rem"`, `"2rem"`, 1) }})
	me := newPerson(t)
	me.run(t, "each item has its own approve and reject, and Apply waits for a mark",
		chromedp.EmulateViewport(1280, 2000),
		chromedp.Navigate(s.url+"?tab=proposals"),
		designPoll(`!!document.querySelector('[data-proposal-row]')`),
		designClick(`[data-action=open-proposal]`),
		designPoll(`document.querySelectorAll('[data-proposal-panel]').length === 3`),
		designPoll(`document.querySelectorAll('[data-proposal-panel] [data-action=mark-approve]').length === 3`),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === true`),
		designClick(item("token:colors.ink", ` [data-action=mark-approve]`)),
		designClick(item("token:colors.signal", ` [data-action=mark-approve]`)),
		designClick(item("token:spacing.md", ` [data-action=mark-reject]`)),
		designPoll(`document.querySelector('`+item("token:colors.ink", ` [data-action=mark-approve]`)+`').getAttribute('aria-pressed') === 'true'`),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === false`),
		designPoll(`document.querySelector('[data-next-version]').textContent.includes('version 2') && document.querySelector('[data-next-version]').textContent.includes('2 approved')`),
		shot("impl-item-review-marked", "light"),
	)
	me.run(t, "apply",
		designClick(`[data-action=apply-decisions]`),
		designPoll(`document.querySelector('[data-proposal-result]')?.textContent.includes('version 2')`),
		designPoll(`document.querySelector('[data-proposal-row] [data-proposal-state]')?.textContent.trim() === 'Partly approved'`),
		designPoll(`document.querySelector('`+item("token:colors.ink", ` [data-item-decision]`)+`').textContent.trim() === 'Approved'`),
		designPoll(`document.querySelector('`+item("token:spacing.md", ` [data-item-decision]`)+`').textContent.trim() === 'Rejected'`),
		designPoll(`!document.querySelector('[data-proposal-bar]')`),
		shot("impl-item-review-partly-approved", "light"),
	)
	got := s.get()
	if got["version"] != float64(2) || got["open_proposals"] != float64(0) {
		t.Fatalf("design system = %v", got)
	}
}

// An item left unmarked stays pending and the proposal stays open; a
// conflicting item needs its own confirmation.
func TestItemReviewInTheBrowserKeepsUnmarkedItemsPendingAndConfirmsEachOverwrite(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	s.proposedItems("Ink and a calmer signal", addInk, calmerSignal)
	record := dsRecord(buttonComponent())
	record["tokens"].([]any)[0] = dsToken("colors", "signal", "color", "oklch(0.6 0.2 30)")
	s.mustSave(1, record)

	me := newPerson(t)
	me.run(t, "a conflicting item cannot be applied until it is confirmed",
		chromedp.EmulateViewport(1280, 1800),
		chromedp.Navigate(s.url+"?tab=proposals"),
		designPoll(`!!document.querySelector('[data-proposal-row]')`),
		designClick(`[data-action=open-proposal]`),
		designPoll(`document.querySelectorAll('[data-proposal-panel]').length === 2`),
		designClick(item("token:colors.signal", ` [data-action=mark-approve]`)),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === true`),
		designClick(item("token:colors.signal", ` [data-field=confirm-overwrite]`)),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === false`),
		designClick(item("token:colors.signal", ` [data-field=confirm-overwrite]`)),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === true`),
		designClick(item("token:colors.signal", ` [data-action=mark-approve]`)), // unmarks it
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === true`),
		designClick(item("token:colors.ink", ` [data-action=mark-approve]`)),
		designClick(`[data-action=apply-decisions]`),
		designPoll(`document.querySelector('[data-proposal-result]')?.textContent.includes('version 3')`),
		designPoll(`document.querySelector('[data-proposal-row] [data-proposal-state]')?.textContent.trim() === 'Waiting'`),
		designPoll(`document.querySelector('`+item("token:colors.signal", ` [data-item-decision]`)+`').textContent.trim() === 'Pending'`),
		designPoll(`document.querySelector('`+item("token:colors.ink", ` [data-item-decision]`)+`').textContent.trim() === 'Approved'`),
	)
	if got := s.get(); got["version"] != float64(3) || got["open_proposals"] != float64(1) {
		t.Fatalf("design system = %v", got)
	}
	me.run(t, "the pending conflicting item is confirmed and approved",
		designClick(item("token:colors.signal", ` [data-action=mark-approve]`)),
		designClick(item("token:colors.signal", ` [data-field=confirm-overwrite]`)),
		designClick(`[data-action=apply-decisions]`),
		designPoll(`document.querySelector('[data-proposal-result]')?.textContent.includes('version 4')`),
		designPoll(`document.querySelector('[data-proposal-row] [data-proposal-state]')?.textContent.trim() === 'Approved'`),
	)
}

// A component whose preview needs a token the person is rejecting, or has not
// approved in the same apply, is flagged before applying, naming the token.
func TestItemReviewInTheBrowserFlagsAComponentWhoseTokenIsNotApproved(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	s.proposedItems("Ink and a chip that uses it", addInk, addChip)
	chip := item("component:chip")
	me := newPerson(t)
	me.run(t, "the chip is flagged until its token is approved with it",
		chromedp.EmulateViewport(1280, 2200),
		chromedp.Navigate(s.url+"?tab=proposals"),
		designPoll(`!!document.querySelector('[data-proposal-row]')`),
		designClick(`[data-action=open-proposal]`),
		designPoll(`document.querySelectorAll('[data-proposal-panel]').length === 2`),
		designClick(chip+` [data-action=mark-approve]`),
		designPoll(`document.querySelector('`+chip+` [data-item-flag]')?.textContent.includes('colors.ink')`),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === true`),
		designClick(item("token:colors.ink", ` [data-action=mark-reject]`)),
		designPoll(`document.querySelector('`+chip+` [data-item-flag]')?.textContent.includes('rejecting')`),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === true`),
		shot("impl-item-review-flag", "light"),
		designClick(item("token:colors.ink", ` [data-action=mark-approve]`)),
		designPoll(`!document.querySelector('`+chip+` [data-item-flag]')`),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === false`),
		designClick(`[data-action=apply-decisions]`),
		designPoll(`document.querySelector('[data-proposal-row] [data-proposal-state]')?.textContent.trim() === 'Approved'`),
	)
	if got := s.get(); got["version"] != float64(2) {
		t.Fatalf("design system = %v", got)
	}
}

// While the design session runs, chosen items go back to it with a note; the
// proposal then waits as a draft until the session resubmits.
func TestItemReviewInTheBrowserSendsItemsBackToARunningSession(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	sessionID := s.proposedItems("Ink and a calmer signal", addInk, calmerSignal)
	me := newPerson(t)
	me.run(t, "an item is marked to send back and the note is written",
		chromedp.EmulateViewport(1280, 2100),
		chromedp.Navigate(s.url+"?tab=proposals"),
		designPoll(`!!document.querySelector('[data-proposal-row]')`),
		designClick(`[data-action=open-proposal]`),
		designPoll(`document.querySelectorAll('[data-proposal-panel] [data-action=mark-back]').length === 2`),
		designPoll(`document.querySelector('[data-action=send-back]').disabled === true`),
		designClick(item("token:colors.signal", ` [data-action=mark-back]`)),
		designPoll(`document.querySelector('[data-action=send-back]').disabled === false`),
		designPoll(`document.querySelector('[data-action=apply-decisions]').disabled === true`),
		chromedp.SetValue(`[data-field=send-back-note]`, "Go halfway to the old colour.", chromedp.ByQuery),
		shot("impl-item-review-send-back", "light"),
		designClick(`[data-action=send-back]`),
		designPoll(`document.querySelector('[data-proposal-row] [data-proposal-state]')?.textContent.trim() === 'Drafting'`),
		designPoll(`document.querySelector('`+item("token:colors.signal", ` [data-item-decision]`)+`').textContent.trim() === 'Sent back'`),
		designPoll(`!document.querySelector('[data-proposal-bar]')`),
		shot("impl-item-review-drafting", "light"),
	)
	// The session resubmits: the proposal waits again with every item pending.
	ctx := context.Background()
	view, err := s.cat.DesignSystems.DesignDraftFiles(ctx, sessionID, s.ds)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range view.Files {
		if f.Path == "tokens.json" {
			if err := s.cat.DesignSystems.WriteDraftFile(ctx, sessionID, s.ds, "tokens.json", []byte(strings.Replace(string(f.Content), "oklch(0.35 0.1 200)", "oklch(0.42 0.1 190)", 1))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.cat.DesignSystems.SubmitDesignProposal(ctx, sessionID, "Halfway"); err != nil {
		t.Fatal(err)
	}
	me.run(t, "the proposal is open again",
		designPoll(`document.querySelector('[data-proposal-row] [data-proposal-state]')?.textContent.trim() === 'Waiting'`),
		designPoll(`getComputedStyle(document.querySelector('`+item("token:colors.signal", ` [data-item-decision]`)+`')).display === 'none'`),
		designPoll(`!!document.querySelector('[data-proposal-bar]')`),
	)
}

// When the session has ended there is nobody to answer: Send back is not
// offered, and Withdraw still is.
func TestItemReviewInTheBrowserOffersNoSendBackForAnEndedSession(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	sessionID := s.proposedItems("Ink and a calmer signal", addInk, calmerSignal)
	if err := s.orch.Stop(context.Background(), sessionID); err != nil {
		t.Fatal(err)
	}
	me := newPerson(t)
	me.run(t, "no Send back on an ended session",
		chromedp.EmulateViewport(1280, 2100),
		chromedp.Navigate(s.url+"?tab=proposals"),
		designPoll(`!!document.querySelector('[data-proposal-row]')`),
		designPoll(`!!document.querySelector('[data-action=withdraw-proposal]')`),
		designPoll(`document.querySelector('[data-proposal-detail]').textContent.includes('session ended')`),
		designClick(`[data-action=open-proposal]`),
		designPoll(`document.querySelectorAll('[data-proposal-panel] [data-action=mark-approve]').length === 2`),
		designPoll(`document.querySelector('[data-action=mark-back]') === null && document.querySelector('[data-action=send-back]') === null`),
	)
}
