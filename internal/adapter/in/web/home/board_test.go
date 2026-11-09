package home

import (
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// A live session with an open interview reads "questions" on the card's line,
// whatever its status says, after approvals and before your turn; the browser's
// agentboard/presentation/view.js orders and words the lines the same way.
func TestACardLineReadsQuestionsForAnOpenInterviewAfterApprovalsAndBeforeYourTurn(t *testing.T) {
	now := time.Date(2026, 11, 8, 14, 0, 0, 0, time.UTC)
	line := func(id string, status domain.SessionStatus, interviews, questions int) domain.BoardSessionLine {
		return domain.BoardSessionLine{SessionID: id, AgentName: id, Role: "delegate", Status: status, StatusSince: now, OpenInterviewCount: interviews, OpenQuestionCount: questions}
	}
	card := domain.BoardCard{
		TaskID: "t1", ProjectID: "p1", TaskTitle: "Interview UI", NeedsYou: true,
		Sessions: []domain.BoardSessionLine{
			line("running", domain.SessionRunning, 0, 0),
			line("turn", domain.SessionIdle, 0, 0),
			line("asking-running", domain.SessionRunning, 1, 3),
			line("approval", domain.SessionWaitingApproval, 1, 2),
			line("asking-idle", domain.SessionIdle, 2, 4),
		},
	}
	v := newCard(card, now)
	var got []string
	for _, l := range append(append([]LineView{}, v.Lines...), v.Hidden...) {
		got = append(got, l.SessionID+":"+l.Word)
	}
	want := []string{"approval:approval", "asking-running:questions", "asking-idle:questions", "turn:your turn", "running:running"}
	if len(got) != len(want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lines = %v, want %v", got, want)
		}
	}
	// Only the word and the chip change: the dot is still the session's own.
	for _, w := range []struct{ id, dot string }{{"asking-running", "running"}, {"asking-idle", "waiting"}} {
		for _, l := range append(append([]LineView{}, v.Lines...), v.Hidden...) {
			if l.SessionID == w.id && (l.Dot != w.dot || l.Tone != "attention" || !l.Chip) {
				t.Errorf("%s = dot %q tone %q chip %v, want dot %q and the attention chip", w.id, l.Dot, l.Tone, l.Chip, w.dot)
			}
		}
	}
}
