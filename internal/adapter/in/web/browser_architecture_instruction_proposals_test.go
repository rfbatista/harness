package web

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	proposalsSection = instructionsPanel + ` [data-proposals-section]`
	// The attention badge on the Instructions & rules tab: its number, or "" when the badge is absent.
	tabBadge = `(document.querySelector('#architecture-tab-instructions [data-proposals-count]')?.textContent.trim() ?? '')`
)

// proposalRig is a rig with two proposals: an open one from the architect's
// task, and a rejected one with its note.
func proposalRig(t *testing.T) *archRig {
	t.Helper()
	rig := instructionRig(t)
	rig.proposals.add(fakeProposal{ID: "p-old", State: "rejected", Summary: "Skill for running the Go tests", Skills: []string{"go-tests"}, TaskID: "t2", TaskTitle: "Test runner", Agent: "claude", Mode: "implement", Note: "Covered by the Makefile targets already."}, 5*24*time.Hour)
	rig.proposals.add(fakeProposal{ID: "p-open", Summary: "Verification skill and feature map", Skills: []string{"verify-harness", "features-harness"}}, 25*time.Minute)
	return rig
}

func proposalRowJS(id, part string) string {
	return `document.querySelector('` + proposalsSection + ` [data-proposal="` + id + `"]')` + part
}

// The tab names how many proposals wait; the section lists them open first,
// each with what the person needs to pick one, and decided ones keep their
// decision and note.
func TestInstructionProposalsAreListedWithTheOpenCountOnTheTab(t *testing.T) {
	rig := proposalRig(t)
	openInstructions(t, rig, "", 1280)
	var badge, order, row, decided, sr string
	rig.run(t,
		poll(`document.querySelectorAll('`+proposalsSection+` [data-proposal]').length === 2`),
		chromedp.Evaluate(tabBadge, &badge),
		chromedp.Evaluate(`[...document.querySelectorAll('`+proposalsSection+` [data-proposal]')].map(e => e.dataset.proposal).join()`, &order),
		chromedp.Evaluate(proposalRowJS("p-open", `.textContent.replace(/\s+/g, ' ')`), &row),
		chromedp.Evaluate(proposalRowJS("p-old", `.textContent.replace(/\s+/g, ' ')`), &decided),
		chromedp.Evaluate(`document.querySelector('#architecture-tab-instructions [data-proposals]').textContent.replace(/\s+/g, ' ').trim()`, &sr),
	)
	if badge != "1" {
		t.Fatalf("tab badge = %q, want the one open proposal", badge)
	}
	if sr != "1 to review" {
		t.Fatalf("the badge reads %q to a screen reader, want %q", sr, "1 to review")
	}
	if order != "p-open,p-old" {
		t.Fatalf("order = %q, open first", order)
	}
	for _, want := range []string{"Verification skill and feature map", "Open", "verify-harness", "features-harness", "1 file", "proposed 25 min ago", "Verification Skill", "software-architect · architect"} {
		if !strings.Contains(row, want) {
			t.Errorf("the open row lacks %q: %q", want, row)
		}
	}
	for _, want := range []string{"Skill for running the Go tests", "Rejected", "go-tests", "rejected", "Test runner", "claude · implement", "Covered by the Makefile targets already."} {
		if !strings.Contains(decided, want) {
			t.Errorf("the decided row lacks %q: %q", want, decided)
		}
	}
	if got := rig.proposals.called(); len(got) == 0 || got[0] != "list_instruction_proposals" {
		t.Fatalf("calls = %v, want the list first", got)
	}
}

// The badge is absent at zero, and shows again as the feed announces proposals.
func TestInstructionProposalBadgeIsAbsentAtZeroAndFollowsTheFeedInEveryTab(t *testing.T) {
	rig := instructionRig(t)
	openInstructions(t, rig, "", 1280)
	var badge string
	var hidden bool
	rig.run(t,
		poll(`!!document.querySelector('`+proposalsSection+`')`),
		chromedp.Evaluate(tabBadge, &badge),
		chromedp.Evaluate(`!document.querySelector('#architecture-tab-instructions [data-proposals]')`, &hidden),
	)
	if badge != "" || !hidden {
		t.Fatalf("with no proposals the badge is %q (absent %v)", badge, hidden)
	}

	// A second tab of the same browser, on the same page.
	other, cancel := chromedp.NewContext(rig.ctx)
	t.Cleanup(cancel)
	if err := chromedp.Run(other); err != nil {
		t.Fatal(err)
	}
	load, stop := context.WithTimeout(other, 15*time.Second)
	err := chromedp.Run(load, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.url("/instructions")), poll(`!!document.querySelector('`+proposalsSection+`')`))
	stop()
	if err != nil {
		t.Fatalf("second tab: %v", err)
	}
	// The browser shares one page event stream across its tabs (ADR 0011).
	waitFor(t, "the shared feed", func() bool { return rig.feed.followers() >= 1 })

	// The first tab is in the background now, where animation frames do not run: poll it on a timer.
	timer := func(js string) chromedp.Action {
		return chromedp.Poll(js, nil, chromedp.WithPollingInterval(50*time.Millisecond), chromedp.WithPollingTimeout(8*time.Second))
	}
	both := func(n string) {
		t.Helper()
		rows := `document.querySelectorAll('` + proposalsSection + ` [data-proposal]').length === ` + n
		rig.run(t, timer(rows), timer(tabBadge+` === '`+n+`'`))
		check, done := context.WithTimeout(other, 10*time.Second)
		defer done()
		if err := chromedp.Run(check, timer(rows), timer(tabBadge+` === '`+n+`'`)); err != nil {
			t.Fatalf("the second tab did not follow the feed to %s: %v", n, err)
		}
	}
	rig.proposals.submit(fakeProposal{Summary: "First draft", Skills: []string{"verify-harness"}})
	both("1")
	rig.proposals.submit(fakeProposal{Summary: "Second draft", Skills: []string{"features-harness"}})
	both("2")
}

// The first count is on the page when it arrives, before any request answers.
func TestInstructionProposalBadgeIsInTheServersFirstRender(t *testing.T) {
	rig := proposalRig(t)
	res := rig.get(t, "/instructions")
	if !strings.Contains(res, `"open_proposals":1`) {
		t.Fatalf("the page seed lacks the open count: %s", res)
	}
}

// get is the page's HTML as the server sends it.
func (r *archRig) get(t *testing.T, path string) string {
	t.Helper()
	res, err := http.Get(r.url(path))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
