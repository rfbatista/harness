package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// fakeBoard serves one seed; Follow is the web page's business, not the handler's.
type fakeBoard struct{ seed *domain.BoardSeed }

func (f fakeBoard) Seed(context.Context) (*domain.BoardSeed, error) { return f.seed, nil }

func (f fakeBoard) Follow(context.Context) (<-chan domain.BoardEvent, error) {
	return make(chan domain.BoardEvent), nil
}

func boardSeed() *domain.BoardSeed {
	ask := "Run: git push origin agent/x"
	feature := "Fast project pages"
	since := now.Add(-4 * time.Minute)
	return &domain.BoardSeed{
		Seq: 7, GeneratedAt: now, ProjectCount: 3, QuietProjects: 2,
		Projects: []domain.BoardProject{{
			ProjectID: "p1", ProjectName: "coding_pool",
			Cards: []domain.BoardCard{{
				TaskID: "t1", ProjectID: "p1", TaskTitle: "Branch guardrails", TaskStatus: "review",
				FeatureID: new(string), FeatureTitle: &feature, NeedsYou: true, NeedsYouSince: &since, LastStatusChangeAt: since,
				Sessions: []domain.BoardSessionLine{
					{SessionID: "s1", AgentName: "backend", Role: "delegate", Status: domain.SessionRunning, StatusSince: now.Add(-12 * time.Minute)},
					{SessionID: "s2", AgentName: "architect", Role: "architect", Status: domain.SessionWaitingApproval, StatusSince: since, WaitingFor: &ask},
				},
			}},
		}},
	}
}

func TestHomeRendersTheAgentBoardFromTheSeed(t *testing.T) {
	h := newTestHandler(t, world{projects: []*domain.Project{{ID: "p1", Name: "coding_pool"}}, board: fakeBoard{boardSeed()}})
	rec := get(t, h, "/")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want the board and no redirect", rec.Code)
	}
	for _, want := range []string{
		`x-data="agentBoardPage"`, `id="agent-board-seed"`, `Branch guardrails`, `Fast project pages`,
		`href="/projects/p1/tasks/t1?session=s2"`, `data-needs-you`, "Needs you", `Run: git push origin agent/x`,
		"1 need you", "2 quiet projects have no live session.", `aria-current="page"`, `>Agent board</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the board lacks %q", want)
		}
	}
	// Approval lines come first, whatever order the server sent.
	if strings.Index(body, `data-session="s2"`) > strings.Index(body, `data-session="s1"`) {
		t.Error("the approval line should lead the running one")
	}
	if strings.Contains(body, `id="rail"`) {
		t.Error("the board draws no rail")
	}
}

func TestHomeSaysNothingIsRunningWhenNoCardIsLive(t *testing.T) {
	seed := &domain.BoardSeed{ProjectCount: 2, QuietProjects: 2}
	rec := get(t, newTestHandler(t, world{projects: []*domain.Project{{ID: "p1", Name: "a"}}, board: fakeBoard{seed}}), "/")
	body := rec.Body.String()
	for _, want := range []string{"Nothing is running", "No live sessions right now.", "Open projects"} {
		if !strings.Contains(body, want) {
			t.Errorf("the empty board lacks %q", want)
		}
	}
}

func TestHomeKeepsTheWelcomePageWithoutProjectsEvenWithABoard(t *testing.T) {
	rec := get(t, newTestHandler(t, world{board: fakeBoard{&domain.BoardSeed{}}}), "/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No projects yet") {
		t.Fatalf("got %d:\n%s", rec.Code, rec.Body)
	}
}

func TestBoardSwappedInPlaceCarriesNoSeedForTheBrowserToRead(t *testing.T) {
	h := newTestHandler(t, world{projects: []*domain.Project{{ID: "p1", Name: "a"}}, board: fakeBoard{boardSeed()}})
	rec := swap(t, h, "/", "app-shell", "HX-Boosted", "true", "HX-Current-URL", "http://x/")
	body := rec.Body.String()
	if !strings.Contains(body, `x-data="agentBoardPage"`) || strings.Contains(body, "agent-board-seed") || strings.Contains(body, "Branch guardrails") {
		t.Errorf("a swapped board should be the page without its seed:\n%s", body)
	}
}
