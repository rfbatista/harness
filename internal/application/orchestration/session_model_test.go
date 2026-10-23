package orchestration

import (
	"context"
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

func TestStartInteractive_ModelFollowsTheSessionsTier(t *testing.T) {
	svc, _ := newInteractiveService(t)
	parent, _ := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})

	cases := []struct {
		name string
		req  InteractiveRequest
		want string
	}{
		{"architect", InteractiveRequest{Mode: "architect"}, "opus"},
		{"started by a session", InteractiveRequest{Mode: "implement", ParentSessionID: parent.ID}, "sonnet"},
		{"architect started by a session", InteractiveRequest{Mode: "architect", ParentSessionID: parent.ID}, "opus"},
		{"started by a person", InteractiveRequest{Mode: "implement"}, ""},
		{"named in the start", InteractiveRequest{Mode: "architect", Model: "haiku"}, "haiku"},
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

func TestStartInteractive_ModelComesFromTheSettings(t *testing.T) {
	svc, _ := newInteractiveService(t)
	settings := modelSettings{domain.SettingModelArchitect: "fable", domain.SettingModelImplementation: domain.ModelDefault}
	svc.Settings = settings
	parent, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	if got := modelOf(launch); got != "fable" {
		t.Errorf("architect --model = %q, want fable", got)
	}
	// "default" leaves the choice to the CLI: no --model at all.
	if _, launch := startInteractive(t, svc, InteractiveRequest{ParentSessionID: parent.ID}); modelOf(launch) != "" {
		t.Errorf("implementation --model = %q, want none", modelOf(launch))
	}

	// A change applies to the next start.
	settings[domain.SettingModelImplementation] = "haiku"
	if _, launch := startInteractive(t, svc, InteractiveRequest{ParentSessionID: parent.ID}); modelOf(launch) != "haiku" {
		t.Errorf("implementation --model after the change = %q, want haiku", modelOf(launch))
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
	settings[domain.SettingModelArchitect] = "haiku"

	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := modelOf(launchOf(t, spec)); got != "opus" {
		t.Errorf("resumed --model = %q, want opus", got)
	}
}
