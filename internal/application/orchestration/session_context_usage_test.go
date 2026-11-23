package orchestration

import (
	"context"
	"strconv"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// scriptedTranscript is a transcript that grows: entries are served by
// position, with the position as cursor, and every page read is counted.
type scriptedTranscript struct {
	entries []ports.TranscriptEntry
	reads   int
	read    []int // entries returned by each read
}

func (f *scriptedTranscript) add(at time.Time, kind, tool, text string) {
	f.entries = append(f.entries, ports.TranscriptEntry{
		Cursor: strconv.Itoa(len(f.entries)), At: at.Format(time.RFC3339), Kind: kind, Tool: tool, Text: text,
	})
}

func (f *scriptedTranscript) ReadTranscript(_, _ string, q ports.TranscriptQuery) (ports.TranscriptPage, error) {
	f.reads++
	lo, hi := 0, len(f.entries)
	switch {
	case q.After != "":
		n, _ := strconv.Atoi(q.After)
		lo = n + 1
		if hi-lo > q.Limit {
			hi = lo + q.Limit
		}
	case q.Before != "":
		n, _ := strconv.Atoi(q.Before)
		hi = n
		if hi-lo > q.Limit {
			lo = hi - q.Limit
		}
	default:
		if hi-lo > q.Limit {
			lo = hi - q.Limit
		}
	}
	page := ports.TranscriptPage{Entries: append([]ports.TranscriptEntry{}, f.entries[lo:hi]...), HasOlder: lo > 0, HasNewer: hi < len(f.entries)}
	f.read = append(f.read, len(page.Entries))
	if len(page.Entries) > 0 {
		page.FirstCursor, page.LastCursor = page.Entries[0].Cursor, page.Entries[len(page.Entries)-1].Cursor
	}
	return page, nil
}

func usageService(t *testing.T) (*Service, *scriptedTranscript) {
	t.Helper()
	svc := contextService(t)
	tr := &scriptedTranscript{}
	svc.TranscriptReader = tr
	storeEntry(t, svc, domain.ContextEntry{
		ID: "e1", Kind: domain.ContextEntryStart, At: time.Unix(10, 0),
		Tools: []domain.ContextTool{
			{Name: "Bash", Source: "claude_code", State: domain.ToolAllowed},
			{Name: "mcp__github__push_files", Source: "mcp", Server: "github", State: domain.ToolOnDemand},
		},
	})
	return svc, tr
}

func syncUsage(t *testing.T, svc *Service) (reasonOK bool) {
	t.Helper()
	changed, err := svc.SyncContextUsage(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func TestSessionContextUsage_CountsEachToolAndSkillFromTheTranscript(t *testing.T) {
	svc, tr := usageService(t)
	t0 := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	tr.add(t0, ports.TranscriptUser, "", "hello")
	tr.add(t0.Add(1*time.Minute), ports.TranscriptToolUse, "Bash", "go test")
	tr.add(t0.Add(2*time.Minute), ports.TranscriptToolUse, "Bash", "go vet")
	tr.add(t0.Add(3*time.Minute), ports.TranscriptToolUse, "Skill", "task-tdd")
	tr.add(t0.Add(4*time.Minute), ports.TranscriptToolResult, "Bash", "ok")

	if !syncUsage(t, svc) {
		t.Fatal("new use reported no change")
	}
	u := contextOf(t, svc, "s1").Usage
	if got := u.Tools["Bash"]; got.Count != 2 || !got.LastAt.Equal(t0.Add(2*time.Minute)) {
		t.Errorf("Bash = %+v", got)
	}
	if got := u.Tools["Skill"]; got.Count != 1 {
		t.Errorf("Skill tool = %+v", got)
	}
	if got := u.Skills["task-tdd"]; got.Count != 1 || !got.LastAt.Equal(t0.Add(3*time.Minute)) {
		t.Errorf("task-tdd = %+v", got)
	}
	if _, ok := u.Tools["hello"]; ok || len(u.Tools) != 2 {
		t.Errorf("only tool_use entries count: %+v", u.Tools)
	}
}

func TestSessionContextUsage_ASecondPassReadsOnlyWhatIsNew(t *testing.T) {
	svc, tr := usageService(t)
	t0 := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		tr.add(t0.Add(time.Duration(i)*time.Minute), ports.TranscriptToolUse, "Bash", "x")
	}
	syncUsage(t, svc)
	if syncUsage(t, svc) {
		t.Error("a pass with nothing new reported a change")
	}
	tr.add(t0.Add(10*time.Minute), ports.TranscriptToolUse, "Bash", "y")
	tr.read = nil
	if !syncUsage(t, svc) {
		t.Error("new use reported no change")
	}
	if len(tr.read) != 1 || tr.read[0] != 1 {
		t.Errorf("the pass read %v entries per page, want one page with the one new entry", tr.read)
	}
	if got := contextOf(t, svc, "s1").Usage.Tools["Bash"]; got.Count != 4 {
		t.Errorf("Bash count = %d, want 4 (no double count)", got.Count)
	}
}

func TestSessionContextUsage_ALongFirstReadFollowsEveryPageBack(t *testing.T) {
	svc, tr := usageService(t)
	t0 := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	for i := 0; i < ports.TranscriptMaxLimit*2+7; i++ {
		tr.add(t0.Add(time.Duration(i)*time.Second), ports.TranscriptToolUse, "Bash", "x")
	}
	syncUsage(t, svc)
	if got := contextOf(t, svc, "s1").Usage.Tools["Bash"].Count; got != ports.TranscriptMaxLimit*2+7 {
		t.Errorf("count = %d", got)
	}
}

func TestSessionContextUsage_ASkillInvocationMakesAChange(t *testing.T) {
	svc, tr := usageService(t)
	t0 := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	tr.add(t0, ports.TranscriptToolUse, "Skill", "task-tdd")
	tr.add(t0.Add(time.Minute), ports.TranscriptToolUse, "Skill", "task-tdd")
	syncUsage(t, svc)
	var invoked []domain.ContextChange
	for _, c := range contextOf(t, svc, "s1").Changes {
		if c.Kind == domain.ContextChangeSkillInvoked {
			invoked = append(invoked, c)
		}
	}
	if len(invoked) != 2 || invoked[0].Subject != "task-tdd" || invoked[0].Detail == "" || !invoked[1].At.Equal(t0) {
		t.Errorf("skill_invoked changes (newest first) = %+v", invoked)
	}
}

func TestSessionContextUsage_OnlyTheFirstUseOfAnOnDemandToolMakesAChange(t *testing.T) {
	svc, tr := usageService(t)
	t0 := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	tr.add(t0, ports.TranscriptToolUse, "Bash", "allowed tool: no change")
	tr.add(t0.Add(time.Minute), ports.TranscriptToolUse, "mcp__github__push_files", "first")
	syncUsage(t, svc)
	tr.add(t0.Add(2*time.Minute), ports.TranscriptToolUse, "mcp__github__push_files", "second")
	syncUsage(t, svc)

	var first []domain.ContextChange
	for _, c := range contextOf(t, svc, "s1").Changes {
		if c.Kind == domain.ContextChangeToolFirstUsed {
			first = append(first, c)
		}
	}
	if len(first) != 1 || first[0].Subject != "mcp__github__push_files" || !first[0].At.Equal(t0.Add(time.Minute)) {
		t.Errorf("tool_first_used changes = %+v", first)
	}
	if got := contextOf(t, svc, "s1").Usage.Tools["mcp__github__push_files"].Count; got != 2 {
		t.Errorf("count = %d", got)
	}
}

func TestSessionContextUsage_ASessionNotRecordedHasNoUsage(t *testing.T) {
	svc := contextService(t)
	tr := &scriptedTranscript{}
	tr.add(time.Now(), ports.TranscriptToolUse, "Bash", "x")
	svc.TranscriptReader = tr
	if syncUsage(t, svc) {
		t.Error("a session with no context reported a change")
	}
	if tr.reads != 0 {
		t.Errorf("read the transcript of a session with no recorded context %d times", tr.reads)
	}
}
