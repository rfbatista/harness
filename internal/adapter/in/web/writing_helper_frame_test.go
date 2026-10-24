package web

import (
	"context"
	"testing"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

func helperServer(t *testing.T, helper func(context.Context) shell.Helper) *server {
	t.Helper()
	deps := Deps{Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "Orion"}}}, Tasks: fakeTickets{}, Sessions: fakeSessions{}, WritingHelper: helper}
	return &server{deps: deps, assets: builtAssets(t)}
}

func TestEveryFrameCarriesWhetherTheWritingHelperCanRun(t *testing.T) {
	for name, want := range map[string]shell.Helper{
		"available":   {Available: true},
		"unavailable": {Reason: "no agent credential", FixURL: "/agent-isolation#credential"},
	} {
		s := helperServer(t, func(context.Context) shell.Helper { return want })
		f, err := s.layout(context.Background(), "Home", "p1", "")
		if err != nil || f.Helper != want {
			t.Errorf("%s: layout helper = %+v, err %v", name, f.Helper, err)
		}
		if got := s.bareFrame("Error").Helper; got != want {
			t.Errorf("%s: bare frame helper = %+v", name, got)
		}
	}
}

func TestWithoutAWritingHelperEveryFrameIsUnavailable(t *testing.T) {
	s := helperServer(t, nil)
	f, _ := s.layout(context.Background(), "Home", "", "")
	if f.Helper != (shell.Helper{}) || s.bareFrame("x").Helper != (shell.Helper{}) {
		t.Fatalf("frame helper = %+v", f.Helper)
	}
}
