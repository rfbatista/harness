package orchestration

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// modelSettings is the settings context as the session model reads it.
type modelSettings map[string]string

func (m modelSettings) Setting(key string) string { return m[key] }

// modelOf is the --model a launch carries, "" when it has none.
func modelOf(l launched) string {
	v, _ := flag(l.Args, "--model")
	return v
}

// The mode alone decides the model, whoever started the session: a person, or
// another session through start_task_session (ADR 0084).
func TestStartInteractive_ModelFollowsTheSessionsMode(t *testing.T) {
	svc, _ := newInteractiveService(t)
	parent, _ := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})

	cases := []struct {
		name string
		req  InteractiveRequest
		want string
	}{
		{"architect", InteractiveRequest{Mode: "architect"}, "opus"},
		{"implement by a person", InteractiveRequest{Mode: "implement"}, "sonnet"},
		{"implement by a session", InteractiveRequest{Mode: "implement", ParentSessionID: parent.ID}, "sonnet"},
		{"architect by a session", InteractiveRequest{Mode: "architect", ParentSessionID: parent.ID}, "opus"},
		{"default by a person", InteractiveRequest{}, ""},
		{"default by a session", InteractiveRequest{ParentSessionID: parent.ID}, ""},
		{"design by a session", InteractiveRequest{Mode: "design", ParentSessionID: parent.ID}, ""},
		{"product", InteractiveRequest{Mode: "product", ProjectID: "p1", RepositoryID: "r1"}, ""},
	}
	for _, c := range cases {
		sess, launch := startInteractive(t, svc, c.req)
		if got := modelOf(launch); got != c.want {
			t.Errorf("%s: --model = %q, want %q", c.name, got, c.want)
		}
		if got := sessionOf(svc, sess.ID); got.Model != c.want {
			t.Errorf("%s: stored model = %q, want %q", c.name, got.Model, c.want)
		}
	}
}

func TestStartInteractive_ModelComesFromTheModesSetting(t *testing.T) {
	svc, _ := newInteractiveService(t)
	settings := modelSettings{
		domain.ModeModelKey(domain.SessionModeArchitect): "fable",
		domain.ModeModelKey(domain.SessionModeImplement): domain.ModelDefault,
		domain.ModeModelKey(domain.SessionModeDesign):    "haiku",
	}
	svc.Settings = settings
	parent, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	if got := modelOf(launch); got != "fable" {
		t.Errorf("architect --model = %q, want fable", got)
	}
	// "default" leaves the choice to the CLI: no --model at all.
	if _, launch := startInteractive(t, svc, InteractiveRequest{Mode: "implement", ParentSessionID: parent.ID}); modelOf(launch) != "" {
		t.Errorf("implement --model = %q, want none", modelOf(launch))
	}
	// A delegate in design mode takes the Design model.
	if _, launch := startInteractive(t, svc, InteractiveRequest{Mode: "design", ParentSessionID: parent.ID}); modelOf(launch) != "haiku" {
		t.Errorf("design --model = %q, want haiku", modelOf(launch))
	}

	// A change applies to the next start.
	settings[domain.ModeModelKey(domain.SessionModeImplement)] = "haiku"
	if _, launch := startInteractive(t, svc, InteractiveRequest{Mode: "implement", ParentSessionID: parent.ID}); modelOf(launch) != "haiku" {
		t.Errorf("implement --model after the change = %q, want haiku", modelOf(launch))
	}
}

// The model a person names for one start wins over the mode's; "default"
// starts with no --model even where the mode pins one.
func TestStartInteractive_AnOverrideWinsOverTheModeModel(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Settings = modelSettings{domain.ModeModelKey(domain.SessionModeArchitect): "fable"}

	sess, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect", Model: "haiku"})
	if got := modelOf(launch); got != "haiku" || sessionOf(svc, sess.ID).Model != "haiku" {
		t.Errorf("override: --model = %q, stored %q; want haiku", got, sessionOf(svc, sess.ID).Model)
	}
	sess, launch = startInteractive(t, svc, InteractiveRequest{Mode: "architect", Model: domain.ModelDefault})
	if got := modelOf(launch); got != "" || sessionOf(svc, sess.ID).Model != "" {
		t.Errorf("default override: --model = %q, stored %q; want none", got, sessionOf(svc, sess.ID).Model)
	}
	// The override is for that session only.
	if _, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"}); modelOf(launch) != "fable" {
		t.Errorf("next start --model = %q, want fable", modelOf(launch))
	}
}

// An unknown model is refused before anything starts.
func TestStartInteractive_AnInvalidOverrideIsRefusedBeforeAnythingStarts(t *testing.T) {
	svc, prov := newInteractiveService(t)
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Model: "gpt-4"})
	if codeOf(err) != "INVALID_INPUT" || !strings.Contains(err.Error(), "model") {
		t.Fatalf("err = %v, want INVALID_INPUT naming model", err)
	}
	if len(prov.created) != 0 {
		t.Errorf("created %d worktrees for a refused start", len(prov.created))
	}
}

// A resumed session keeps the model it started with, whatever the settings
// say now.
func TestResumeInteractive_KeepsTheModelItStartedWith(t *testing.T) {
	svc, _ := newInteractiveService(t)
	settings := modelSettings{}
	svc.Settings = settings
	sess, _ := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	settings[domain.ModeModelKey(domain.SessionModeArchitect)] = "haiku"

	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := modelOf(launchOf(t, spec)); got != "opus" {
		t.Errorf("resumed --model = %q, want opus", got)
	}
}
