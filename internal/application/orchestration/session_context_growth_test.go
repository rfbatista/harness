package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var growthT0 = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

// hookedTranscript is a scriptedTranscript that lets a test act just after
// each page is read: the transcript growing, or another pass running.
type hookedTranscript struct {
	*scriptedTranscript
	after func(read int)
	err   error
}

func (h *hookedTranscript) ReadTranscript(dir, id string, q ports.TranscriptQuery) (ports.TranscriptPage, error) {
	if h.err != nil {
		return ports.TranscriptPage{}, h.err
	}
	page, err := h.scriptedTranscript.ReadTranscript(dir, id, q)
	if h.after != nil {
		h.after(h.scriptedTranscript.reads)
	}
	return page, err
}

func hookedService(t *testing.T) (*Service, *hookedTranscript) {
	t.Helper()
	svc, tr := usageService(t)
	h := &hookedTranscript{scriptedTranscript: tr}
	svc.TranscriptReader = h
	return svc, h
}

func bashCount(t *testing.T, svc *Service) int {
	t.Helper()
	return contextOf(t, svc, "s1").Usage.Tools["Bash"].Count
}

func TestSessionContextGrowth_AnEntryAppendedWhileTheFirstPassReadsIsCountedOnceByTheNextPass(t *testing.T) {
	svc, h := hookedService(t)
	n := ports.TranscriptMaxLimit*2 + 7
	for i := 0; i < n; i++ {
		h.add(growthT0.Add(time.Duration(i)*time.Second), ports.TranscriptToolUse, "Bash", "x")
	}
	grown := false
	h.after = func(read int) {
		if read == 1 && !grown { // the newest page is read: the agent keeps working
			grown = true
			for i := 0; i < 3; i++ {
				h.add(growthT0.Add(time.Hour+time.Duration(i)*time.Second), ports.TranscriptToolUse, "Bash", "late")
			}
		}
	}
	syncUsage(t, svc)
	if got := bashCount(t, svc); got != n {
		t.Fatalf("after the first pass count = %d, want the %d entries read (the 3 that arrived mid-read wait)", got, n)
	}
	h.after = nil
	if !syncUsage(t, svc) {
		t.Fatal("the entries that arrived mid-read were never picked up")
	}
	if got := bashCount(t, svc); got != n+3 {
		t.Errorf("count = %d, want %d: nothing lost, nothing counted twice", got, n+3)
	}
}

func TestSessionContextGrowth_AnEntryAppendedWhileALaterPassFollowsItsCursorIsNotLostOrCountedTwice(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0, ports.TranscriptToolUse, "Bash", "first")
	syncUsage(t, svc)
	total := 1
	// A burst longer than one page, with more arriving while it is paged through.
	for i := 0; i < ports.TranscriptMaxLimit+5; i++ {
		h.add(growthT0.Add(time.Minute+time.Duration(i)*time.Second), ports.TranscriptToolUse, "Bash", "x")
		total++
	}
	grown := false
	h.after = func(int) {
		if !grown {
			grown = true
			h.add(growthT0.Add(time.Hour), ports.TranscriptToolUse, "Bash", "mid")
			total++
		}
	}
	syncUsage(t, svc)
	h.after = nil
	syncUsage(t, svc)
	syncUsage(t, svc)
	if got := bashCount(t, svc); got != total {
		t.Errorf("count = %d, want %d", got, total)
	}
}

func TestSessionContextGrowth_TwoPassesAtOnceCountEachEntryOnce(t *testing.T) {
	svc, h := hookedService(t)
	for i := 0; i < 4; i++ {
		h.add(growthT0.Add(time.Duration(i)*time.Second), ports.TranscriptToolUse, "Bash", "x")
	}
	inner := false
	h.after = func(int) {
		if !inner { // a second pass starts from the same cursor and finishes first
			inner = true
			if _, err := svc.SyncContextUsage(context.Background(), "s1"); err != nil {
				t.Error(err)
			}
		}
	}
	syncUsage(t, svc)
	h.after = nil
	syncUsage(t, svc)
	if got := bashCount(t, svc); got != 4 {
		t.Errorf("count = %d, want 4 (the pass that lost the race must not add its entries again)", got)
	}
}

func TestSessionContextGrowth_ANewResumeEntryDoesNotMakeTheNextPassReadTheWholeTranscriptAgain(t *testing.T) {
	svc, h := hookedService(t)
	for i := 0; i < 3; i++ {
		h.add(growthT0.Add(time.Duration(i)*time.Second), ports.TranscriptToolUse, "Bash", "x")
	}
	syncUsage(t, svc)
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, growthT0.Add(time.Hour)))
	h.add(growthT0.Add(2*time.Hour), ports.TranscriptToolUse, "Bash", "y")
	h.read = nil
	syncUsage(t, svc)
	if got := bashCount(t, svc); got != 4 {
		t.Errorf("count = %d, want 4", got)
	}
	if len(h.read) != 1 || h.read[0] != 1 {
		t.Errorf("pages read after the resume = %v, want one page of the one new entry", h.read)
	}
}

func TestSessionContextGrowth_EntriesThatAreNotToolCallsMoveTheCursorWithoutAChange(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0, ports.TranscriptUser, "", "hello")
	h.add(growthT0.Add(time.Second), ports.TranscriptToolResult, "Bash", "ok")
	if syncUsage(t, svc) {
		t.Error("a pass with no tool call reported a change")
	}
	h.read = nil
	if syncUsage(t, svc) {
		t.Error("the same entries again reported a change")
	}
	if len(h.read) != 1 || h.read[0] != 0 {
		t.Errorf("the second pass read %v: the cursor did not move past the entries with no tool call", h.read)
	}
	h.add(growthT0.Add(time.Minute), ports.TranscriptToolUse, "Bash", "x")
	if !syncUsage(t, svc) || bashCount(t, svc) != 1 {
		t.Errorf("a tool call after them was not counted once: %d", bashCount(t, svc))
	}
}

func TestSessionContextGrowth_ATranscriptNotWrittenYetChangesNothingAndIsReadFromTheStartOnceItAppears(t *testing.T) {
	svc, h := hookedService(t)
	h.err = &domain.StructuredError{Code: "SESSION_TRANSCRIPT_MISSING", Message: "no transcript"}
	if ok, err := svc.SyncContextUsage(context.Background(), "s1"); ok || err != nil {
		t.Fatalf("missing transcript: %v %v", ok, err)
	}
	h.err = nil
	h.add(growthT0, ports.TranscriptToolUse, "Bash", "x")
	h.add(growthT0.Add(time.Second), ports.TranscriptToolUse, "Bash", "y")
	syncUsage(t, svc)
	if got := bashCount(t, svc); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
}

func TestSessionContextGrowth_AnyOtherReadErrorIsReturnedAndLeavesTheCountsAlone(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0, ports.TranscriptToolUse, "Bash", "x")
	syncUsage(t, svc)
	h.err = &domain.StructuredError{Code: "SESSION_TRANSCRIPT_UNREADABLE", Message: "boom"}
	h.add(growthT0.Add(time.Second), ports.TranscriptToolUse, "Bash", "y")
	if ok, err := svc.SyncContextUsage(context.Background(), "s1"); ok || err == nil {
		t.Errorf("an unreadable transcript: changed=%v err=%v", ok, err)
	}
	if got := bashCount(t, svc); got != 1 {
		t.Errorf("count = %d after a failed pass", got)
	}
	h.err = nil
	syncUsage(t, svc)
	if got := bashCount(t, svc); got != 2 {
		t.Errorf("count = %d after the next pass, want 2", got)
	}
}

func TestSessionContextGrowth_AToolOnDemandUsedTwiceInOnePassMakesOneFirstUseAtTheFirstTime(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0.Add(5*time.Minute), ports.TranscriptToolUse, "mcp__github__push_files", "a")
	h.add(growthT0.Add(9*time.Minute), ports.TranscriptToolUse, "mcp__github__push_files", "b")
	syncUsage(t, svc)
	var first []domain.ContextChange
	for _, c := range contextOf(t, svc, "s1").Changes {
		if c.Kind == domain.ContextChangeToolFirstUsed {
			first = append(first, c)
		}
	}
	if len(first) != 1 || !first[0].At.Equal(growthT0.Add(5*time.Minute)) || first[0].Detail == "" {
		t.Errorf("tool_first_used = %+v", first)
	}
}

func TestSessionContextGrowth_AToolThatWasOnDemandInAnyEntryHasAFirstUseEvenIfALaterEntryAllowsIt(t *testing.T) {
	svc, h := hookedService(t)
	e := entryWith("e2", domain.ContextEntryResume, growthT0.Add(time.Hour))
	e.Tools = []domain.ContextTool{{Name: "mcp__github__push_files", Source: "mcp", Server: "github", State: domain.ToolAllowed}}
	storeEntry(t, svc, e)
	h.add(growthT0.Add(2*time.Hour), ports.TranscriptToolUse, "mcp__github__push_files", "x")
	syncUsage(t, svc)
	found := false
	for _, c := range contextOf(t, svc, "s1").Changes {
		found = found || c.Kind == domain.ContextChangeToolFirstUsed
	}
	if !found {
		t.Error("e1 said push_files was on demand; its first use is a change")
	}
}

func TestSessionContextGrowth_AToolThatWasNeverOnDemandOrUnknownMakesNoFirstUseChange(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0, ports.TranscriptToolUse, "Edit", "an unlisted claude code tool")
	h.add(growthT0.Add(time.Second), ports.TranscriptToolUse, "Bash", "allowed")
	syncUsage(t, svc)
	for _, c := range contextOf(t, svc, "s1").Changes {
		if c.Kind == domain.ContextChangeToolFirstUsed {
			t.Errorf("unexpected %+v", c)
		}
	}
}

func TestSessionContextGrowth_LastAtNeverMovesBackwards(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0.Add(10*time.Minute), ports.TranscriptToolUse, "Bash", "x")
	syncUsage(t, svc)
	h.add(growthT0.Add(time.Minute), ports.TranscriptToolUse, "Bash", "older than the last")
	syncUsage(t, svc)
	u := contextOf(t, svc, "s1").Usage.Tools["Bash"]
	if u.Count != 2 || !u.LastAt.Equal(growthT0.Add(10*time.Minute)) {
		t.Errorf("usage = %+v", u)
	}
}

func TestSessionContextGrowth_ASkillCallWithNoNameCountsTheToolButNoSkill(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0, ports.TranscriptToolUse, "Skill", "")
	h.add(growthT0.Add(time.Second), ports.TranscriptToolUse, "", "a call with no tool name")
	syncUsage(t, svc)
	v := contextOf(t, svc, "s1")
	if v.Usage.Tools["Skill"].Count != 1 || len(v.Usage.Skills) != 0 || len(v.Usage.Tools) != 1 {
		t.Errorf("usage = %+v", v.Usage)
	}
	for _, c := range v.Changes {
		if c.Kind == domain.ContextChangeSkillInvoked {
			t.Errorf("unexpected %+v", c)
		}
	}
}

func TestSessionContextGrowth_ACallWithAnUnreadableTimeStillCounts(t *testing.T) {
	svc, h := hookedService(t)
	h.entries = append(h.entries, ports.TranscriptEntry{Cursor: "0", At: "not a time", Kind: ports.TranscriptToolUse, Tool: "Bash"})
	if !syncUsage(t, svc) {
		t.Fatal("no change")
	}
	if u := contextOf(t, svc, "s1").Usage.Tools["Bash"]; u.Count != 1 || u.LastAt.IsZero() {
		t.Errorf("usage = %+v", u)
	}
}

func TestSessionContextGrowth_WhatAToolWasCalledWithIsNeverKept(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0, ports.TranscriptToolUse, "Bash", "curl -H 'Authorization: Bearer "+secretHeader+"' https://x")
	h.add(growthT0.Add(time.Second), ports.TranscriptToolUse, "mcp__github__push_files", "token="+secretEnv)
	syncUsage(t, svc)
	assertNoSecret(t, "served context", contextOf(t, svc, "s1"))
	stored, _ := svc.Contexts.Load(context.Background(), "s1")
	assertNoSecret(t, "stored context", stored)
}

func TestSessionContextGrowth_DifferentSkillsAreCountedApartAndEachCallIsAChange(t *testing.T) {
	svc, h := hookedService(t)
	h.add(growthT0, ports.TranscriptToolUse, "Skill", "task-tdd")
	h.add(growthT0.Add(time.Second), ports.TranscriptToolUse, "Skill", "task-test")
	h.add(growthT0.Add(2*time.Second), ports.TranscriptToolUse, "Skill", "task-tdd")
	syncUsage(t, svc)
	v := contextOf(t, svc, "s1")
	if v.Usage.Skills["task-tdd"].Count != 2 || v.Usage.Skills["task-test"].Count != 1 {
		t.Errorf("skills = %+v", v.Usage.Skills)
	}
	n := 0
	for _, c := range v.Changes {
		if c.Kind == domain.ContextChangeSkillInvoked && strings.HasPrefix(c.Subject, "task-") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("skill_invoked changes = %d, want 3", n)
	}
}
