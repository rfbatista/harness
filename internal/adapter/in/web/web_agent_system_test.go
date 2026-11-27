package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestAgentSystemPageMountsItsIslandAndTheRailLinksIt(t *testing.T) {
	h := newRenderedHandler(t, board())

	rec := get(t, h, "/projects/p1/agent-system")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<div data-island="agent-system" data-island-rendered="false"></div>`,
		`<script type="application/json" data-island-props="agent-system">`,
		`"project_id":"p1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Agent system page is missing %q", want)
		}
	}
	if got := currentProjectPage(body); got != "/projects/p1/agent-system" {
		t.Errorf("the rail marks %q as the open page, want the Agent system link", got)
	}
}

func TestAgentSystemIsInTheRailAfterUsageAndBeforeTheProductOwner(t *testing.T) {
	w := board()
	w.usage = fakeUsage{}
	body := get(t, newRenderedHandler(t, w), "/projects/p1").Body.String()
	usage, agent, owner := strings.Index(body, `href="/projects/p1/usage"`), strings.Index(body, `href="/projects/p1/agent-system"`), strings.Index(body, `href="/projects/p1/product-owner"`)
	if usage < 0 || agent < usage || owner < agent {
		t.Errorf("rail order wrong: usage %d, agent system %d, product owner %d", usage, agent, owner)
	}
}

func TestAgentSystemPageOfAMissingProjectIs404(t *testing.T) {
	if rec := get(t, newTestHandler(t, board()), "/projects/nope/agent-system"); rec.Code != http.StatusNotFound {
		t.Errorf("got %d", rec.Code)
	}
}
