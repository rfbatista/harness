package web

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
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

const (
	proposalPane = instructionsPanel + ` [data-proposal-pane]`
	newSkillPath = ".claude/skills/verify-harness/SKILL.md"
	removedPath  = ".claude/skills/features-harness/old.md"
	changedPath  = ".claude/skills/features-harness/README.md"
	changedRead  = "# Features\n\nA map of the harness.\n"
	changedAfter = "# Features\n\nA map of the harness.\n\nVerify before you finish.\n"
)

// filesRig is a rig with one open proposal that creates a skill file, changes
// one and removes one, all under .claude/skills/<skill>/ (ADR 0092 allows only
// skill files in a proposal): the three ops.
func filesRig(t *testing.T) *archRig {
	t.Helper()
	rig := instructionRig(t)
	rig.files.seed(changedPath, "skill", changedRead)
	rig.files.seed(removedPath, "skill", "Superseded by README.md.\n")
	rig.proposals.add(fakeProposal{
		ID: "p-files", Summary: "Verification skill", Skills: []string{"verify-harness"},
		Files: []fakeProposalFile{
			{Path: newSkillPath, Op: "create", Content: "---\nname: verify-harness\n---\nLaunch, drive, record.\n"},
			{Path: changedPath, Op: "change", BaseVersion: 1, BaseContent: changedRead, Content: changedAfter},
			{Path: removedPath, Op: "remove", BaseVersion: 1, BaseContent: "Superseded by README.md.\n"},
		},
	}, 10*time.Minute)
	return rig
}

func openProposal(t *testing.T, rig *archRig, id string) {
	t.Helper()
	rig.run(t,
		poll(`!!`+proposalRowJS(id, `.querySelector('[data-open-proposal]')`)),
		chromedp.Evaluate(proposalRowJS(id, `.querySelector('[data-open-proposal]').click()`), nil),
		poll(`!!document.querySelector('`+proposalPane+` .diff-view .file')`),
		// x-show reveals the column a tick after its content exists.
		poll(`document.querySelector('`+proposalPane+`').checkVisibility()`),
	)
}

func fileJS(path, part string) string {
	return `document.querySelector('` + proposalPane + ` .diff-view .file[data-path="` + path + `"]')` + part
}

func lineCount(path, kind string) string {
	return `document.querySelectorAll('` + proposalPane + ` .diff-view .file[data-path="` + path + `"] .line[data-kind="` + kind + `"]').length`
}

// A proposal opens beside its list with each file's op and a diff of what it
// would change; Approve saves every file and the workspace shows them.
func TestInstructionProposalShowsEachFilesDiffAndApprovingSavesThem(t *testing.T) {
	rig := filesRig(t)
	openInstructions(t, rig, "", 1280)
	openProposal(t, rig, "p-files")
	var ops string
	var created, changedAdd, changedDel, removed int
	rig.run(t,
		chromedp.Evaluate(`[...document.querySelectorAll('`+proposalPane+` .diff-view .file')].map(f => f.dataset.path + ':' + f.dataset.op).join()`, &ops),
		chromedp.Evaluate(lineCount(newSkillPath, "add"), &created),
		chromedp.Evaluate(lineCount(changedPath, "add"), &changedAdd),
		chromedp.Evaluate(lineCount(changedPath, "del"), &changedDel),
		chromedp.Evaluate(lineCount(removedPath, "del"), &removed),
	)
	if want := newSkillPath + ":create," + changedPath + ":change," + removedPath + ":remove"; ops != want {
		t.Fatalf("files = %q, want %q", ops, want)
	}
	if created != 4 || changedAdd != 2 || changedDel != 0 || removed != 1 {
		t.Fatalf("diff lines: create +%d, change +%d −%d, remove −%d; want +4, +2 −0, −1", created, changedAdd, changedDel, removed)
	}

	rig.run(t, chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-approve]').click()`, nil), poll(`!!document.querySelector('`+proposalPane+` [data-approved-notice]')`))
	var notice, state string
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-approved-notice]').textContent.replace(/\s+/g, ' ')`, &notice),
		chromedp.Evaluate(proposalRowJS("p-files", `.dataset.state`), &state),
	)
	if !strings.Contains(notice, "2 files saved as new versions, 1 removed") || state != "approved" {
		t.Fatalf("after approve: notice %q, row state %q", notice, state)
	}
	if v := rig.files.version(changedPath); v != 2 {
		t.Fatalf("%s is at version %d, want 2", changedPath, v)
	}
	// The files list beside follows the saves the server announced.
	rig.run(t,
		poll(`!!document.querySelector('`+instructionsPanel+` [data-open-file][aria-label="Open `+newSkillPath+`"]')`),
		poll(`!document.querySelector('`+instructionsPanel+` [data-open-file][aria-label="Open `+removedPath+`"]')`),
		poll(tabBadge+` === ''`),
	)
	if got := rig.proposals.approveCalls(); got != 1 {
		t.Fatalf("approve was called %d times, want 1", got)
	}
}

// A file the person saved since the draft read it is marked stale at once; approving
// is refused with the stale paths and a suggestion, and nothing is saved.
func TestInstructionProposalMarksAStaleFileAndRefusesTheApproval(t *testing.T) {
	rig := filesRig(t)
	openInstructions(t, rig, "", 1280)
	openProposal(t, rig, "p-files")
	var fresh bool
	rig.run(t, chromedp.Evaluate(`!`+fileJS(changedPath, `.hasAttribute('data-stale')`), &fresh))
	if !fresh {
		t.Fatal("a file nobody saved is marked stale")
	}

	rig.files.saveElsewhere(changedPath, "Someone else wrote this.\n")
	var head, notice, refusal, paths string
	rig.run(t,
		poll(`!!`+fileJS(changedPath, `.matches('[data-stale="true"]')`)),
		poll(`!!document.querySelector('`+proposalPane+` [data-stale-notice]')`),
		chromedp.Evaluate(fileJS(changedPath, `.querySelector('.head [data-stale]').textContent.trim()`), &head),
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-stale-notice]').textContent.replace(/\s+/g, ' ')`, &notice),
	)
	if head != "stale · v1 read, v2 saved" || !strings.Contains(notice, "1 file changed since the draft read it") {
		t.Fatalf("stale head %q, notice %q", head, notice)
	}
	var rowBadge string
	rig.run(t,
		poll(`!!`+proposalRowJS("p-files", `.querySelector('[data-stale-files]')`)),
		chromedp.Evaluate(proposalRowJS("p-files", `.querySelector('[data-stale-files]').textContent.trim()`), &rowBadge),
	)
	if rowBadge != "1 stale file" {
		t.Fatalf("the row's stale badge = %q", rowBadge)
	}

	rig.run(t,
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-approve]').click()`, nil),
		poll(`!!document.querySelector('`+proposalPane+` [data-refusal]')`),
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-refusal]').textContent.replace(/\s+/g, ' ')`, &refusal),
		chromedp.Evaluate(`[...document.querySelectorAll('`+proposalPane+` [data-refusal] .refused-paths li')].map(li => li.textContent.replace(/\s+/g, ' ').trim()).join('|')`, &paths),
	)
	for _, want := range []string{"Approval refused: 1 file was saved since the draft read it. Nothing was saved.", "INSTRUCTION_CHANGED", "send it back"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal lacks %q: %q", want, refusal)
		}
	}
	if paths != changedPath+" · v1 read, v2 saved" {
		t.Fatalf("refused paths = %q", paths)
	}
	if _, ok := rig.files.content(newSkillPath); ok {
		t.Fatal("a refused approval saved the new skill")
	}
	var state string
	rig.run(t, chromedp.Evaluate(proposalRowJS("p-files", `.dataset.state`), &state))
	if state != "open" {
		t.Fatalf("the proposal is %q after a refusal, want open", state)
	}
}

// The stream echo of the person's own approval reaches the page before the
// POST answers; the pane still ends approved, with one request and no error.
func TestInstructionProposalApproveIsGuardedAgainstItsOwnEchoAndDoubleClicks(t *testing.T) {
	rig := filesRig(t)
	rig.proposals.approveDelay = 700 * time.Millisecond
	openInstructions(t, rig, "", 1280)
	openProposal(t, rig, "p-files")
	var disabled bool
	rig.run(t,
		chromedp.Evaluate(`(() => { const b = document.querySelector('`+proposalPane+` [data-approve]'); b.click(); b.click(); return true })()`, nil),
		poll(`document.querySelector('`+proposalPane+` [data-approve]')?.disabled === true || !!document.querySelector('`+proposalPane+` [data-approved-notice]')`),
		poll(`!!document.querySelector('`+proposalPane+` [data-approved-notice]')`),
		chromedp.Evaluate(`!!document.querySelector('`+proposalPane+` [data-refusal]') || !!document.querySelector('`+instructionsPanel+` [data-proposals-error]:not([style*="none"])')`, &disabled),
	)
	if disabled {
		t.Fatal("an error showed beside a successful approval")
	}
	if got := rig.proposals.approveCalls(); got != 1 {
		t.Fatalf("approve was called %d times, want 1", got)
	}
}

// ── reject and send back ────────────────────────────────────────────────────

func openAsk(t *testing.T, rig *archRig, which string) {
	t.Helper()
	rig.run(t,
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-open-`+which+`]').click()`, nil),
		poll(`!!document.querySelector('`+proposalPane+` [data-ask]')`),
	)
}

func typeNote(t *testing.T, rig *archRig, text string) {
	t.Helper()
	rig.run(t, chromedp.Evaluate(`(() => { const n = document.querySelector('`+proposalPane+` [data-ask] textarea'); n.value = `+jsString(text)+`; n.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil))
}

func submitAsk(t *testing.T, rig *archRig) {
	t.Helper()
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-ask] [type=submit]').click()`, nil))
}

// Send back asks for a note under the decision row, with focus in the note; the
// proposal then reads sent back with the note, in the list and the pane, and
// the badge drops.
func TestInstructionProposalSendBackNeedsANoteAndKeepsIt(t *testing.T) {
	rig := filesRig(t)
	openInstructions(t, rig, "", 1280)
	openProposal(t, rig, "p-files")
	openAsk(t, rig, "sendback")
	var focused, expanded bool
	rig.run(t,
		poll(`document.activeElement === document.querySelector('`+proposalPane+` [data-ask] textarea')`),
		chromedp.Evaluate(`document.activeElement === document.querySelector('`+proposalPane+` [data-ask] textarea')`, &focused),
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-open-sendback]').getAttribute('aria-expanded') === 'true'`, &expanded),
	)
	if !focused || !expanded {
		t.Fatalf("focus in the note %v, Send back expanded %v", focused, expanded)
	}

	// No note: the field says so and nothing is sent.
	var problem string
	submitAsk(t, rig)
	rig.run(t,
		poll(`!!document.querySelector('`+proposalPane+` [data-ask-error]')`),
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-ask-error]').textContent.trim()`, &problem),
	)
	if !strings.Contains(problem, "A note is required") {
		t.Fatalf("error = %q", problem)
	}
	if n := rig.proposals.callsTo("send_back_instruction_proposal"); n != 0 {
		t.Fatalf("send back was called %d times with no note", n)
	}

	typeNote(t, rig, "Wait for the listening line.")
	submitAsk(t, rig)
	var state, rowNote, paneState, decided string
	rig.run(t,
		poll(proposalRowJS("p-files", `.dataset.state`)+` === 'sent_back'`),
		chromedp.Evaluate(proposalRowJS("p-files", `.querySelector('[data-proposal-note]').textContent.replace(/\s+/g, ' ').trim()`), &rowNote),
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-pane-state]').textContent.trim()`, &paneState),
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` .entry-kv').textContent.replace(/\s+/g, ' ')`, &decided),
		chromedp.Evaluate(proposalRowJS("p-files", `.dataset.state`), &state),
		poll(tabBadge+` === ''`),
		poll(`!document.querySelector('`+proposalPane+` [data-ask]') && !document.querySelector('`+proposalPane+` [data-approve]')`),
	)
	if rowNote != "Note Wait for the listening line." || paneState != "Sent back" || !strings.Contains(decided, "sent back · Wait for the listening line.") {
		t.Fatalf("row note %q, pane state %q, decided %q", rowNote, paneState, decided)
	}
	if body := rig.proposals.lastBody("send_back_instruction_proposal"); body["note"] != "Wait for the listening line." || body["proposal_id"] != "p-files" {
		t.Fatalf("send back body = %v", body)
	}
}

// Reject needs no note; without one none is sent. A proposal already decided
// elsewhere answers PROPOSAL_DECIDED and the pane shows what was decided.
func TestInstructionProposalRejectNeedsNoNoteAndADecidedProposalShowsItsDecision(t *testing.T) {
	rig := filesRig(t)
	rig.proposals.add(fakeProposal{ID: "p-second", Summary: "Second", Skills: []string{"go-tests"}, Files: []fakeProposalFile{{Path: ".claude/skills/go-tests/SKILL.md", Op: "create", Content: "x\n"}}}, 5*time.Minute)
	openInstructions(t, rig, "", 1280)

	openProposal(t, rig, "p-files")
	openAsk(t, rig, "reject")
	submitAsk(t, rig)
	var state string
	rig.run(t, poll(proposalRowJS("p-files", `.dataset.state`)+` === 'rejected'`), chromedp.Evaluate(proposalRowJS("p-files", `.dataset.state`), &state))
	if body := rig.proposals.lastBody("reject_instruction_proposal"); body == nil {
		t.Fatal("reject was not called")
	} else if _, has := body["note"]; has {
		t.Fatalf("a reject with no note sent one: %v", body)
	}

	// Another tab approves the second one after this page opened it; this page has not heard yet.
	rig.run(t, chromedp.Evaluate(proposalRowJS("p-second", `.querySelector('[data-open-proposal]').click()`), nil), poll(`!!document.querySelector('`+proposalPane+` [data-open-reject]') && document.querySelector('`+proposalPane+` [data-pane-state]').textContent.trim() === 'Open'`))
	rig.proposals.setStateQuietly("p-second", "approved")
	openAsk(t, rig, "reject")
	submitAsk(t, rig)
	var stillAsking bool
	rig.run(t,
		poll(`document.querySelector('`+proposalPane+` [data-pane-state]').textContent.trim() === 'Approved'`),
		chromedp.Evaluate(`!!document.querySelector('`+proposalPane+` [data-ask]') || !!document.querySelector('`+proposalPane+` [data-decide-error]:not([style*="none"])')`, &stillAsking),
	)
	if stillAsking {
		t.Fatal("the pane still asks, or shows an error, after the proposal turned out decided")
	}
}

// ── the notification link and the keyboard ──────────────────────────────────

// tabTo presses Tab until the element matching sel has focus, or fails after a few presses.
func tabTo(t *testing.T, rig *archRig, sel string) {
	t.Helper()
	for i := 0; i < 24; i++ {
		var on bool
		rig.run(t, chromedp.Evaluate(`document.activeElement?.matches('`+sel+`') === true`, &on))
		if on {
			return
		}
		rig.run(t, chromedp.KeyEvent(kb.Tab))
	}
	t.Fatalf("Tab did not reach %s in 24 presses", sel)
}

// The notification's link opens the workspace with the proposal already open.
func TestInstructionProposalDeepLinkOpensTheProposal(t *testing.T) {
	rig := filesRig(t)
	openInstructions(t, rig, "?proposal=p-files", 1280)
	var title, viewing string
	rig.run(t,
		poll(`!!document.querySelector('`+proposalPane+` .diff-view .file') && document.querySelector('`+proposalPane+`').checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('`+proposalPane+` header h3').textContent.trim()`, &title),
		chromedp.Evaluate(proposalRowJS("p-files", `.dataset.viewing`), &viewing),
	)
	if title != "Verification skill" || viewing != "true" {
		t.Fatalf("deep link opened %q, row viewing %q", title, viewing)
	}
	// A link to a proposal that is gone says so, and the list still works.
	rig.run(t, chromedp.Navigate(rig.url("/instructions?proposal=nope")), poll(`!!document.querySelector('`+instructionsPanel+` [data-proposal-error]')`))
}

// The list, the files and the decision are reachable with Tab and Enter, and
// what happens is announced in a status region.
func TestInstructionProposalCanBeReviewedAndSentBackWithTheKeyboardAlone(t *testing.T) {
	rig := filesRig(t)
	openInstructions(t, rig, "", 1280)
	rig.run(t,
		poll(`!!`+proposalRowJS("p-files", `.querySelector('[data-open-proposal]')`)),
		chromedp.Evaluate(proposalRowJS("p-files", `.querySelector('[data-open-proposal]').focus()`), nil),
		chromedp.KeyEvent("\r"),
		poll(`document.activeElement?.hasAttribute('data-pane-heading') === true`),
	)
	var live string
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+instructionsPanel+` [data-proposals-live]').textContent.trim()`, &live))
	if live != "Opened the proposal: Verification skill, 3 files." {
		t.Fatalf("status = %q", live)
	}
	var unnamed int
	rig.run(t, chromedp.Evaluate(`[...document.querySelectorAll('`+proposalsSection+` button')].filter(b => !(b.getAttribute('aria-label') || b.textContent.trim())).length`, &unnamed))
	if unnamed != 0 {
		t.Fatalf("%d buttons have no name", unnamed)
	}

	tabTo(t, rig, `[data-approve]`)
	tabTo(t, rig, `[data-open-sendback]`)
	rig.run(t, chromedp.KeyEvent("\r"), poll(`document.activeElement === document.querySelector('`+proposalPane+` [data-ask] textarea')`), chromedp.KeyEvent("Wait for the listening line."))
	tabTo(t, rig, `[data-ask] [type=submit]`)
	rig.run(t, chromedp.KeyEvent("\r"), poll(proposalRowJS("p-files", `.dataset.state`)+` === 'sent_back'`))
	rig.run(t, poll(`document.querySelector('`+instructionsPanel+` [data-proposals-live]').textContent.includes('sent back')`))
	if body := rig.proposals.lastBody("send_back_instruction_proposal"); body["note"] != "Wait for the listening line." {
		t.Fatalf("send back body = %v", body)
	}

	// Close returns to the proposal's row. Focus was lost with the ask; start from the pane's heading.
	rig.run(t, chromedp.Evaluate(`document.querySelector('`+proposalPane+` [data-pane-heading]').focus()`, nil))
	tabTo(t, rig, `[data-close-proposal]`)
	rig.run(t, chromedp.KeyEvent("\r"), poll(`document.activeElement === `+proposalRowJS("p-files", `.querySelector('[data-open-proposal]')`)))
}

// ── the repository's entry on the project's repositories page ───────────────

const repositoryBadge = `(document.querySelector('a[href$="/repositories/r1/architecture"]')?.parentElement.querySelector('[data-proposals-count]')?.textContent.trim() ?? '')`

// The repository's entry carries the open-proposal count beside its Architecture
// link: in the server's first render, changing as the feed announces, and absent
// at zero.
func TestInstructionProposalBadgeIsBesideTheRepositoryEntry(t *testing.T) {
	rig := proposalRig(t)
	res, err := http.Get(rig.srv.URL + "/projects/p1/repositories")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "data-proposals") || !strings.Contains(string(body), `"open_proposals":{"r1":1}`) {
		t.Fatalf("the server's first render lacks the count: %s", body)
	}

	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.srv.URL+"/projects/p1/repositories"),
		poll(`!!document.querySelector('a[href$="/repositories/r1/architecture"]')`),
		poll(repositoryBadge+` === '1'`),
	)
	waitFor(t, "the page's feed", func() bool { return rig.feed.followers() >= 1 })
	var sr string
	rig.run(t, chromedp.Evaluate(`document.querySelector('a[href$="/repositories/r1/architecture"]').parentElement.querySelector('[data-proposals]').textContent.replace(/\s+/g, ' ').trim()`, &sr))
	if sr != "1 to review" {
		t.Fatalf("the badge reads %q", sr)
	}
	rig.proposals.submit(fakeProposal{Summary: "Another", Skills: []string{"go-tests"}})
	rig.run(t, poll(repositoryBadge+` === '2'`))
	rig.proposals.decideElsewhere("p-open", "approved")
	rig.proposals.decideElsewhere("p3", "rejected")
	rig.run(t, poll(repositoryBadge+` === ''`))
}
