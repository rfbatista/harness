package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// fakeInterviews holds each session's interview list.
type fakeInterviews map[string]*domain.InterviewList

func (f fakeInterviews) ListInterviews(_ context.Context, ids []string) (map[string]*domain.InterviewList, error) {
	out := map[string]*domain.InterviewList{}
	for _, id := range ids {
		if l, ok := f[id]; ok {
			out[id] = l
		}
	}
	return out, nil
}

// The task page's first paint holds each of its sessions' interviews in the
// sessions seed, as GET /api/sessions/{id}/interviews would answer them, and
// none of another task's.
func TestTaskPageSeedsTheInterviewsOfItsSessions(t *testing.T) {
	w := board()
	w.interviews = fakeInterviews{
		"s1": {Seq: 4, Interviews: []*domain.Interview{{
			ID: "iv-1", SessionID: "s1", AgentName: "Reviewer", Title: "Storage", AskedAt: now, State: domain.InterviewOpen,
			Questions: []domain.InterviewQuestion{{Title: "Which store?", Kind: domain.QuestionSingle, Options: []domain.InterviewOption{{Label: "SQLite"}}}},
		}}},
		"s9": {Seq: 1, Interviews: []*domain.Interview{{ID: "iv-9", SessionID: "s9", AskedAt: now.Add(time.Hour), State: domain.InterviewOpen}}},
	}
	rec := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "iv-9") {
		t.Error("the page carries an interview of another project's session")
	}
	seeded, _ := seedOf(t, rec.Body.String(), "sessions-seed")["interviews"].(map[string]any)
	s1, _ := seeded["s1"].(map[string]any)
	list, _ := s1["interviews"].([]any)
	if s1["seq"] != float64(4) || len(list) != 1 {
		t.Fatalf("interviews of s1 = %v, want seq 4 and one interview", seeded["s1"])
	}
	iv := list[0].(map[string]any)
	if iv["id"] != "iv-1" || iv["agent_name"] != "Reviewer" || iv["state"] != "open" || iv["ended_at"] != nil {
		t.Errorf("interview = %v", iv)
	}
	// A session with none still has its own empty list at seq 0, so a panel
	// can tell "nothing asked" from "not read".
	s2, _ := seeded["s2"].(map[string]any)
	if empty, ok := s2["interviews"].([]any); !ok || len(empty) != 0 || s2["seq"] != float64(0) {
		t.Errorf("interviews of s2 = %v, want [] at seq 0", seeded["s2"])
	}
	if _, ok := seeded["s9"]; ok {
		t.Error("the seed holds a session of another project")
	}
}

// Without a reader the page seeds none and still paints.
func TestTaskPageSeedsNoInterviewsWithoutAReader(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got, ok := seedOf(t, rec.Body.String(), "sessions-seed")["interviews"].(map[string]any); !ok || len(got) != 0 {
		t.Errorf("interviews = %v, want {}", got)
	}
}
