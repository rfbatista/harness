package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestScorePageMountsItsIslandAndTheRailLinksIt(t *testing.T) {
	h := newRenderedHandler(t, board())

	rec := get(t, h, "/projects/p1/score")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<div data-island="score" data-island-rendered="false"></div>`,
		`<script type="application/json" data-island-props="score">`,
		`"project_id":"p1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Score page is missing %q", want)
		}
	}
	if got := currentProjectPage(body); got != "/projects/p1/score" {
		t.Errorf("the rail marks %q as the open page, want the Score link", got)
	}
}

func TestScoreIsInTheRailAfterAgentSystemAndBeforeTheProductOwner(t *testing.T) {
	body := get(t, newRenderedHandler(t, board()), "/projects/p1").Body.String()
	agent, score, owner := strings.Index(body, `href="/projects/p1/agent-system"`), strings.Index(body, `href="/projects/p1/score"`), strings.Index(body, `href="/projects/p1/product-owner"`)
	if agent < 0 || score < agent || owner < score {
		t.Errorf("rail order wrong: agent system %d, score %d, product owner %d", agent, score, owner)
	}
}

func TestScorePageOfAMissingProjectIs404(t *testing.T) {
	if rec := get(t, newTestHandler(t, board()), "/projects/nope/score"); rec.Code != http.StatusNotFound {
		t.Errorf("got %d", rec.Code)
	}
}
