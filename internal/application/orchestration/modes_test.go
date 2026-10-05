package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// pluginSkill reads a skill's SKILL.md out of the session's --plugin-dir.
func pluginSkill(t *testing.T, l launched, name string) string {
	t.Helper()
	dir, ok := flag(l.Args, "--plugin-dir")
	if !ok {
		t.Fatalf("no --plugin-dir: %q", l.Args)
	}
	b, err := os.ReadFile(filepath.Join(dir, "skills", name, "SKILL.md"))
	if err != nil {
		t.Fatalf("skill %s not in the plugin dir: %v", name, err)
	}
	return string(b)
}

func TestStartInteractive_ArchitectMode(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.tickets.(fakeTickets)["tk1"].Description = "Users export their data as CSV."
	sess, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})

	if sess.Mode != domain.SessionModeArchitect {
		t.Fatalf("mode = %q, want architect", sess.Mode)
	}
	if got := sessionOf(svc, sess.ID); got == nil || got.Mode != domain.SessionModeArchitect {
		t.Fatalf("mode not persisted: %+v", got)
	}
	if body := pluginSkill(t, launch, "task-architecture"); !strings.Contains(body, "name: task-architecture") {
		t.Errorf("task-architecture SKILL.md has no frontmatter name:\n%s", body)
	}
	prompt := launch.Spec.Prompt
	for _, want := range []string{"task-architecture skill", "tk1: Ship the thing", "Users export their data as CSV."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("first message lacks %q:\n%s", want, prompt)
		}
	}
}

func TestStartInteractive_ArchitectModeKeepsTypedPromptAndAgentSkills(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.catalog.Agents.(*fakeResolver).agents = map[string]*domain.Agent{"a1": {
		ID: "a1",
		Skills: []domain.Skill{{
			Name:  "Review",
			Files: []domain.SkillFile{{Path: domain.SkillFileName, Content: domain.SkillMarkdownFor("review", "reviews", "Review.")}},
		}},
	}}
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "architect", Prompt: "Only the backend for now."})

	pluginSkill(t, launch, "review")
	pluginSkill(t, launch, "task-architecture")
	if p := launch.Spec.Prompt; !strings.HasPrefix(p, "Use the task-architecture skill") || !strings.HasSuffix(p, "Only the backend for now.") {
		t.Errorf("first message = %q, want the kickoff followed by the typed prompt", p)
	}
}

func TestStartInteractive_DefaultModeAddsNothing(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, launch := startInteractive(t, svc, InteractiveRequest{Prompt: "hi"})
	if sess.Mode != domain.SessionModeDefault {
		t.Errorf("mode = %q, want default", sess.Mode)
	}
	if _, ok := flag(launch.Args, "--plugin-dir"); ok {
		t.Errorf("plain claude in default mode got a plugin dir: %q", launch.Args)
	}
	if launch.Spec.Prompt != "hi" {
		t.Errorf("prompt = %q, want it untouched", launch.Spec.Prompt)
	}
}

func TestStartInteractive_UnknownMode(t *testing.T) {
	svc, prov := newInteractiveService(t)
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "wizard"})
	if codeOf(err) != "INVALID_INPUT" {
		t.Fatalf("err = %v, want INVALID_INPUT", err)
	}
	if len(prov.created) != 0 {
		t.Fatalf("a rejected mode provisioned a worktree: %v", prov.created)
	}
}

func TestResumeInteractive_KeepsTheModeSkills(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	pluginSkill(t, launchOf(t, spec), "task-architecture")
}
