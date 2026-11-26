//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Review — visual QA on the repository's declared UI (ADR 0011, Glossary
// "Visual QA"): started for a session through the API, or by an architect with
// start_task_session; the qa session starts the app through a service request.

const declaredUI = `{"start_command":"npm run dev","url":"http://localhost:5173","routes":["/","/login"],"viewports":["1280x800","390x844"],"ui_paths":["web/**"]}`

func (e *shellE2E) startVisualQA(sessionID string) (int, map[string]any) {
	e.t.Helper()
	res, err := http.Post(e.srv.URL+"/api/sessions/"+sessionID+"/visual-qa", "application/json", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res.StatusCode, body
}

func (e *shellE2E) qaSessions(ticketID string) []*domain.Session {
	var out []*domain.Session
	for _, s := range e.sessions.List(e.ctx, ports.SessionFilter{TicketID: ticketID}) {
		if s.Mode == domain.SessionModeQA {
			out = append(out, s)
		}
	}
	return out
}

func (e *shellE2E) declareUI() {
	e.t.Helper()
	if _, err := e.agentSystem.SaveRepository(e.ctx, e.project, e.repo, map[string]json.RawMessage{"review.visual_qa.ui": json.RawMessage(declaredUI)}); err != nil {
		e.t.Fatal(err)
	}
}

func TestVisualQAOnARepositoryWithNoUIDeclarationIsRefusedWithItsReasonAndStartsNothing(t *testing.T) {
	e, author := criticWorld(t)

	code, body := e.startVisualQA(author.ID)
	if code != http.StatusConflict || body["code"] != "VISUAL_QA_NOT_DECLARED" || !strings.Contains(body["error"].(string), "declares no UI") {
		t.Fatalf("refusal = %d %v", code, body)
	}
	if got := e.qaSessions(author.TicketID); len(got) != 0 {
		t.Errorf("%d qa sessions started on a refusal", len(got))
	}
	if code, _ := e.startVisualQA("no-such-session"); code != http.StatusNotFound {
		t.Errorf("an unknown session: %d, want 404", code)
	}
}

func TestVisualQAOnADeclaredUIStartsAQASessionWhoseBriefCarriesTheDeclaration(t *testing.T) {
	e, author := criticWorld(t)
	e.orch.AgentSystem = e.agentSystem // the guard fake gave the author its session; qa briefs read the real store
	e.declareUI()

	code, body := e.startVisualQA(author.ID)
	if code != http.StatusCreated {
		t.Fatalf("start = %d %v", code, body)
	}
	qas := e.qaSessions(author.TicketID)
	if len(qas) != 1 || body["id"] != qas[0].ID {
		t.Fatalf("qa sessions = %+v, answered %v", qas, body["id"])
	}
	if qas[0].Access != domain.AccessReadOnly || qas[0].WorkspaceID != author.WorkspaceID {
		t.Errorf("qa runs read-only on the author's worktree, got %q on %q", qas[0].Access, qas[0].WorkspaceID)
	}
	brief := e.openingOf(qas[0].ID)
	for _, want := range []string{
		"npm run dev", "http://localhost:5173", "/login", "1280x800", "390x844",
		"service request", "stop service request", "screenshot", "task/designs/", "design assets", "previous visual QA pass",
		"`Visual QA: <task title>`", "route", "viewport", "`pass` or `fail`",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the qa brief lacks %q", want)
		}
	}
}

func TestAQASessionAnArchitectStartsGetsTheSameDeclarationAndWithoutOneTheBriefIsUnchanged(t *testing.T) {
	e, author := criticWorld(t)
	e.orch.AgentSystem = e.agentSystem // the guard fake gave the author its session; qa briefs read the real store
	start := func() *domain.Session {
		t.Helper()
		qa, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: author.TicketID, Mode: "qa"})
		if err != nil {
			t.Fatal(err)
		}
		return qa
	}

	plain := e.openingOf(start().ID)
	if strings.Contains(plain, "visual QA pass") || strings.Contains(plain, "npm run dev") {
		t.Errorf("a repository with no declaration got a visual QA brief: %q", plain)
	}
	e.declareUI()
	declared := e.openingOf(start().ID)
	for _, want := range []string{"npm run dev", "/login", "390x844", "visual QA pass"} {
		if !strings.Contains(declared, want) {
			t.Errorf("the architect-started qa brief lacks %q", want)
		}
	}
}
