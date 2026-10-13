//go:build !windows

package orchestration

import (
	"encoding/json"
	"strings"
	"testing"
)

const testShellBase = "http://127.0.0.1:8080/api/sessions/"

// claudeSettings is the hooks part of a --settings payload.
type claudeSettings struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct{ Type, Command string }
	} `json:"hooks"`
}

func settingsOf(t *testing.T, args []string) claudeSettings {
	t.Helper()
	raw, ok := flag(args, "--settings")
	if !ok {
		t.Fatalf("no --settings in %q", args)
	}
	var s claudeSettings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("--settings is not JSON: %v\n%s", err, raw)
	}
	return s
}

// hookCommand is the one command of event's hook group for matcher.
func (s claudeSettings) hookCommand(event, matcher string) (string, bool) {
	for _, g := range s.Hooks[event] {
		if g.Matcher == matcher && len(g.Hooks) == 1 && g.Hooks[0].Type == "command" {
			return g.Hooks[0].Command, true
		}
	}
	return "", false
}

// The edit proxy's hooks sit next to the session's own: Edit, Write,
// MultiEdit and NotebookEdit are checked before and committed after, Bash is
// checked before, all against this session's shell endpoints.
func TestAnInteractiveTaskSessionsSettingsCarryTheEditProxyHooks(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	w := withShells(t, svc)
	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	s := settingsOf(t, launch.Args)

	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		if len(s.Hooks[event]) == 0 {
			t.Errorf("the session's own %s hook is gone: %+v", event, s.Hooks)
		}
	}
	base := testShellBase + sess.ID + "/shell"
	for _, h := range []struct{ event, matcher, url string }{
		{"PreToolUse", "Edit|Write|MultiEdit|NotebookEdit", base + "/check?hook=PreToolUse"},
		{"PostToolUse", "Edit|Write|MultiEdit|NotebookEdit", base + "/commit?hook=PostToolUse"},
		{"PreToolUse", "Bash", base + "/check?hook=PreToolUse"},
	} {
		cmd, ok := s.hookCommand(h.event, h.matcher)
		if !ok {
			t.Errorf("no %s hook on %s: %+v", h.event, h.matcher, s.Hooks)
			continue
		}
		if !strings.Contains(cmd, "curl") || !strings.Contains(cmd, h.url) {
			t.Errorf("%s %s hook does not curl %s: %s", h.event, h.matcher, h.url, cmd)
		}
	}
}

func TestASessionWithoutAShellHasNoEditProxyHooks(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	_, launch := startInteractive(t, svc, InteractiveRequest{})
	s := settingsOf(t, launch.Args)
	if len(s.Hooks["PreToolUse"]) != 0 || len(s.Hooks["PostToolUse"]) != 0 {
		t.Errorf("a session with no shell got edit-proxy hooks: %+v", s.Hooks)
	}
	if len(s.Hooks["SessionStart"]) == 0 {
		t.Errorf("the session's own hooks are gone: %+v", s.Hooks)
	}
}
