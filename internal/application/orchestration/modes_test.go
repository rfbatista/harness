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
	return pluginFile(t, l, name, "SKILL.md")
}

// pluginFile reads one file of a skill out of the session's --plugin-dir.
func pluginFile(t *testing.T, l launched, name, file string) string {
	t.Helper()
	dir, ok := flag(l.Args, "--plugin-dir")
	if !ok {
		t.Fatalf("no --plugin-dir: %q", l.Args)
	}
	b, err := os.ReadFile(filepath.Join(dir, "skills", name, file))
	if err != nil {
		t.Fatalf("skill %s: %s not in the plugin dir: %v", name, file, err)
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
	body := pluginSkill(t, launch, "task-architecture")
	if !strings.Contains(body, "name: task-architecture") {
		t.Errorf("task-architecture SKILL.md has no frontmatter name:\n%s", body)
	}
	// Sessions copy the templates literally, so they must be HTML pages.
	for _, want := range []string{"<!doctype html>", "<title>Contract: ", "<title>Architecture: ", "HTML page"} {
		if !strings.Contains(body, want) {
			t.Errorf("task-architecture SKILL.md lacks %q: its document templates must be HTML", want)
		}
	}
	if spec := pluginSkill(t, launch, "task-to-spec"); !strings.Contains(spec, "<!doctype html>") || !strings.Contains(spec, "<title>Spec: ") {
		t.Error("task-to-spec SKILL.md lacks the HTML Spec template")
	}
	if !strings.Contains(body, "already notify the person") {
		t.Error("task-architecture SKILL.md does not say review requests and the review status already notify the person")
	}
	if strings.Contains(body, "```\n## <Application>") || strings.Contains(body, "```\n**Type:** REST API") || strings.Contains(body, "<title>Spec: ") {
		t.Error("task-architecture SKILL.md still carries Markdown document templates")
	}
	prompt := launch.Spec.Prompt
	for _, want := range []string{"task-architecture skill", "tk1: Ship the thing", "Users export their data as CSV.", "add_task_comment",
		"task-grill-with-docs", "task-to-spec", "mode implement"} {
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

func TestStartInteractive_DesignMode(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.tickets.(fakeTickets)["tk1"].Description = "A pricing card in three states."
	sess, launch := startInteractive(t, svc, InteractiveRequest{Mode: "design", Prompt: "Start with the hover state."})

	if sess.Mode != domain.SessionModeDesign {
		t.Fatalf("mode = %q, want design", sess.Mode)
	}
	if got := sessionOf(svc, sess.ID); got == nil || got.Mode != domain.SessionModeDesign {
		t.Fatalf("mode not persisted: %+v", got)
	}
	body := pluginSkill(t, launch, "design-artifacts")
	for _, want := range []string{"name: design-artifacts", "publish_artifact", "design/", "self-contained", "kind", "url", "Design tab", "HTML page"} {
		if !strings.Contains(body, want) {
			t.Errorf("design-artifacts SKILL.md lacks %q", want)
		}
	}
	p := launch.Spec.Prompt
	if !strings.HasPrefix(p, "Use the design-artifacts skill on task tk1: Ship the thing") || !strings.Contains(p, "A pricing card in three states.") ||
		!strings.Contains(p, "publish_artifact") || !strings.Contains(p, "add_task_comment") || !strings.HasSuffix(p, "Start with the hover state.") {
		t.Errorf("first message = %q", p)
	}
	// The design skill must not drag the architect one along.
	if _, err := os.Stat(filepath.Join(mustFlag(t, launch.Args, "--plugin-dir"), "skills", "task-architecture")); err == nil {
		t.Error("design mode attached the architect skill")
	}
}

func TestResumeInteractive_KeepsTheDesignSkill(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Mode: "design"})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	pluginSkill(t, launchOf(t, spec), "design-artifacts")
}

func mustFlag(t *testing.T, args []string, name string) string {
	t.Helper()
	v, ok := flag(args, name)
	if !ok {
		t.Fatalf("no %s in %q", name, args)
	}
	return v
}

// The architect's specs and contracts are what other tasks build on; its
// skill says they can be moved to project level, with the tool that does it.
func TestStartInteractive_ArchitectSkillNamesProjectDocuments(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	body := pluginSkill(t, launch, "task-architecture")
	for _, want := range []string{"mcp__task__move_document_to_project", "mcp__task__list_project_documents", "project level"} {
		if !strings.Contains(body, want) {
			t.Errorf("task-architecture SKILL.md lacks %q", want)
		}
	}
}

// The designer reuses the project's assets before drawing new ones, and keeps
// what other tasks will reuse at project level; its skill and opening prompt
// say so, with the tools.
func TestStartInteractive_DesignSkillNamesProjectAssets(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "design"})
	body := pluginSkill(t, launch, "design-artifacts")
	for _, want := range []string{"mcp__task__list_project_artifacts", "mcp__task__move_artifact_to_project", "mcp__task__move_artifact_to_task",
		"mcp__task__attach_artifact_to_task", "mcp__task__detach_artifact_from_task",
		"ARTIFACT_IN_PROJECT", "ARTIFACT_NOT_PROMOTABLE", "ARTIFACT_NOT_ON_TASK", "ARTIFACT_NOT_IN_PROJECT", "ARTIFACT_PRODUCER_TASK", "project level"} {
		if !strings.Contains(body, want) {
			t.Errorf("design-artifacts SKILL.md lacks %q", want)
		}
	}
	for _, want := range []string{"list_project_artifacts", "move_artifact_to_project", "attach_artifact_to_task"} {
		if !strings.Contains(launch.Spec.Prompt, want) {
			t.Errorf("design prompt lacks %q:\n%s", want, launch.Spec.Prompt)
		}
	}
}

// The architect points specs at the project's existing design assets.
func TestStartInteractive_ArchitectSkillNamesProjectAssets(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	body := pluginSkill(t, launch, "task-architecture")
	for _, want := range []string{"mcp__task__list_project_artifacts", "mcp__task__attach_artifact_to_task"} {
		if !strings.Contains(body, want) {
			t.Errorf("task-architecture SKILL.md lacks %s", want)
		}
	}
}

// The architect coordinates its delegates and owns the task status: its skill
// and opening prompt name the channel tools, the turns that wake it, and the
// rule that only it moves the status, with a reason.
func TestStartInteractive_ArchitectCoordinatesDelegates(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	body := pluginSkill(t, launch, "task-architecture")
	for _, want := range []string{
		"mcp__task__reply_to_session", "mcp__task__list_task_messages", "mcp__task__request_user_review",
		"mcp__task__withdraw_user_review", "mcp__task__list_review_requests", "mcp__task__set_status_check",
		"mcp__task__list_status_checks", "mcp__task__update_task_status",
		"[task message", "[status check", "review_response", "verdict", "reason", "status_check_minutes",
		"## Coordinating Delegates",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task-architecture SKILL.md lacks %q", want)
		}
	}
	// The prompt it hands each planning agent tells it to report back.
	if !strings.Contains(body, "message_architect") || !strings.Contains(body, "Do not change the task status") {
		t.Error("task-architecture delegation prompt does not tell the delegate to report to the architect")
	}
	for _, want := range []string{"update_task_status", "reason", "reply_to_session", "request_user_review", "set_status_check", "[status check"} {
		if !strings.Contains(launch.Spec.Prompt, want) {
			t.Errorf("architect prompt lacks %q:\n%s", want, launch.Spec.Prompt)
		}
	}
}

// Both mode skills tell the agent to read large documents and assets in
// parts, with the tools for it.
func TestStartInteractive_SkillsReadInParts(t *testing.T) {
	svc, _ := newInteractiveService(t)
	for mode, skill := range map[string]string{"architect": "task-architecture", "implement": "task-implement-spec", "design": "design-artifacts"} {
		_, launch := startInteractive(t, svc, InteractiveRequest{Mode: mode})
		body := pluginSkill(t, launch, skill)
		want := []string{"mcp__task__search_documents", "`section`", "`offset`/`limit`", "never the whole"}
		if skill == "task-architecture" || skill == "task-implement-spec" {
			want = append(want, "mcp__task__edit_task_document", "expected_version")
		} else {
			want = append(want, "mcp__task__read_artifact", "include_artifacts")
		}
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s SKILL.md lacks %q", skill, w)
			}
		}
	}
}

// The architect grills before it specs and specs before it delegates, and
// hands every spec to a delegate in implement mode.
func TestStartInteractive_ArchitectStepsInOrder(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	body := pluginSkill(t, launch, "task-architecture")
	loop := body[strings.Index(body, "## The Architect's Loop"):]
	grill, spec, deleg := strings.Index(loop, "**Grill the person**"), strings.Index(loop, "with the `task-to-spec` skill"), strings.Index(loop, "**Delegate** in `implement` mode")
	if grill < 0 || spec < 0 || deleg < 0 || !(grill < spec && spec < deleg) {
		t.Errorf("architect loop is not grill → spec → delegate (at %d, %d, %d)", grill, spec, deleg)
	}
	for _, want := range []string{"Grill before you spec", `mode: "implement"`, "Ticket:", "task-implement-spec"} {
		if !strings.Contains(body, want) {
			t.Errorf("task-architecture SKILL.md lacks %q", want)
		}
	}
	if strings.Contains(body, "Plan: ") {
		t.Error("task-architecture SKILL.md still asks delegates for a Plan document")
	}
	grilling := pluginSkill(t, launch, "task-grill-with-docs")
	for _, want := range []string{"name: task-grill-with-docs", "frontier", "Glossary: <project>", "ADR: NNNN", "mcp__task__move_document_to_project", "confirms"} {
		if !strings.Contains(grilling, want) {
			t.Errorf("task-grill-with-docs SKILL.md lacks %q", want)
		}
	}
	for _, file := range []string{"GLOSSARY-FORMAT.md", "ADR-FORMAT.md"} {
		if !strings.Contains(pluginFile(t, launch, "task-grill-with-docs", file), "<!doctype html>") {
			t.Errorf("task-grill-with-docs %s has no HTML template", file)
		}
	}
	toSpec := pluginSkill(t, launch, "task-to-spec")
	for _, want := range []string{"User stories", "Testing decisions", "Out of scope", "seams"} {
		if !strings.Contains(toSpec, want) {
			t.Errorf("task-to-spec SKILL.md lacks %q", want)
		}
	}
}

func TestStartInteractive_ImplementMode(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, launch := startInteractive(t, svc, InteractiveRequest{Mode: "implement", Prompt: `Implement "Spec: backend — CSV export".`})

	if sess.Mode != domain.SessionModeImplement {
		t.Fatalf("mode = %q, want implement", sess.Mode)
	}
	body := pluginSkill(t, launch, "task-implement-spec")
	for _, want := range []string{"name: task-implement-spec", "Ticket: <application> — NN", "<!doctype html>", "review_request", "verdict: approved",
		"mcp__task__message_architect", "task-tdd", "task-code-review", `isolation: "worktree"`, "Never move the task status"} {
		if !strings.Contains(body, want) {
			t.Errorf("task-implement-spec SKILL.md lacks %q", want)
		}
	}
	if tdd := pluginSkill(t, launch, "task-tdd"); !strings.Contains(tdd, "Testing decisions") {
		t.Error("task-tdd does not take its seams from the spec's Testing decisions")
	}
	for _, file := range []string{"tests.md", "mocking.md"} {
		pluginFile(t, launch, "task-tdd", file)
	}
	if review := pluginSkill(t, launch, "task-code-review"); !strings.Contains(review, "Spec: <application>") || strings.Contains(review, "setup-matt-pocock-skills") {
		t.Error("task-code-review does not review against the Spec task document")
	}
	p := launch.Spec.Prompt
	if !strings.HasPrefix(p, "Use the task-implement-spec skill on task tk1: Ship the thing") || !strings.Contains(p, "message_architect") ||
		!strings.Contains(p, "approved") || !strings.HasSuffix(p, `Implement "Spec: backend — CSV export".`) {
		t.Errorf("first message = %q", p)
	}
	if _, err := os.Stat(filepath.Join(mustFlag(t, launch.Args, "--plugin-dir"), "skills", "task-architecture")); err == nil {
		t.Error("implement mode attached the architect skill")
	}
}

func TestResumeInteractive_KeepsTheImplementSkills(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Mode: "implement"})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	pluginFile(t, launchOf(t, spec), "task-tdd", "mocking.md")
}
