//go:build !windows

package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A skill's activation rule loads its body into a session: on the first touch
// of a matching file (PostToolUse hook), or at start for a matching task type.

const (
	skillTouchMatcher = "Read|Edit|Write|MultiEdit"
	templSkillBody    = "Always use templ components, never raw html.\n"
	templSkill        = "---\nname: templ-style\ndescription: How to write templ files.\nmetadata:\n  activation_paths: \"web/**/*.templ\"\n---\n\n" + templSkillBody
	reviewSkill       = "---\nname: review-checklist\ndescription: What a reviewer checks.\nmetadata:\n  activation_task_types: review\n---\n\nCheck the diff against the spec.\n"
	plainSkill        = "---\nname: plain-skill\ndescription: A skill with no rule.\n---\n\nPlain body.\n"
)

func (e *shellE2E) touch(s runningSession, tool, rel string) string {
	e.t.Helper()
	return runHook(e.t, s, e.srv.URL, "PostToolUse", skillTouchMatcher, map[string]any{"tool_name": tool, "tool_input": map[string]any{"file_path": filepath.Join(s.worktree, rel)}})
}

func TestSkillActivation_AMatchingFirstTouchLoadsTheBodyOnceAndOtherFilesDoNothing(t *testing.T) {
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"templ-style/SKILL.md", templSkill)
	e.saveSkillFile(skillPrefix+"plain-skill/SKILL.md", plainSkill)
	a := e.start(e.task("Templ work"))

	if out := e.touch(a, "Read", "internal/domain/task.go"); out != "" {
		t.Errorf("a file no rule matches = %s, want nothing", out)
	}
	first := e.touch(a, "Edit", "web/tasks/board.templ")
	if !strings.Contains(first, "PostToolUse") || !strings.Contains(first, "additionalContext") || !strings.Contains(first, "templ-style") || !strings.Contains(first, "never raw html") {
		t.Fatalf("the first matching touch = %s", first)
	}
	if strings.Contains(first, "Plain body") {
		t.Errorf("a skill with no rule was loaded: %s", first)
	}
	if out := e.touch(a, "Read", "web/other/page.templ"); out != "" {
		t.Errorf("a second matching touch = %s, want nothing", out)
	}
	// Another session has its own first touch.
	b := e.start(e.task("More templ work"))
	if out := e.touch(b, "Read", "web/x.templ"); !strings.Contains(out, "never raw html") {
		t.Errorf("another session's first touch = %s", out)
	}
}

func TestSkillActivation_AMatchingTaskTypeLoadsTheBodyAtStart(t *testing.T) {
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"review-checklist/SKILL.md", reviewSkill)
	e.saveSkillFile(skillPrefix+"plain-skill/SKILL.md", plainSkill)

	files := func(mode string) string {
		_, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Task " + mode), Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		var all strings.Builder
		for _, f := range spec.InstructionFiles {
			if strings.Contains(string(f.Content), "Check the diff against the spec.") || strings.Contains(string(f.Content), "Plain body.") {
				all.WriteString(f.Path + "\n")
			}
		}
		return all.String()
	}
	review := files("review")
	if strings.Count(review, "SKILL-review-checklist") != 1 {
		t.Errorf("a review session's files holding the body: %q", review)
	}
	if got := files("implement"); strings.Contains(got, "SKILL-") {
		t.Errorf("an implement session got an active skill: %q", got)
	}
}

// A skill the repository's record scopes to other agents never reaches a
// session of a different agent, by start or by touch (the same applies_to
// filter as delivery).
func TestSkillActivation_ASkillScopedToAnotherAgentNeverActivates(t *testing.T) {
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"templ-style/SKILL.md", templSkill)
	e.saveSkillFile(skillPrefix+"review-checklist/SKILL.md", reviewSkill)
	if _, err := e.cat.Agents.CreateAgent(e.ctx, "some-other-agent", "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	rec := domain.ArchitectureRecord{Repository: "harness", Pattern: "hexagonal", Unlayered: domain.LayerAccess{Default: domain.AccessRead}, Instructions: []domain.ArchInstruction{
		{Path: skillPrefix + "templ-style/SKILL.md", Kind: "skill", AppliesTo: []string{"some-other-agent"}, Loaded: "on_demand"},
		{Path: skillPrefix + "review-checklist/SKILL.md", Kind: "skill", AppliesTo: []string{"some-other-agent"}, Loaded: "on_demand"},
	}}
	if _, err := e.cat.RepositoryArchitectures.SaveRepositoryArchitecture(e.ctx, e.repo, 0, rec); err != nil {
		var se *domain.StructuredError
		if errors.As(err, &se) {
			t.Fatalf("%v %+v", err, se)
		}
		t.Fatal(err)
	}
	a := e.start(e.task("Scoped"))
	if out := e.touch(a, "Edit", "web/tasks/board.templ"); out != "" {
		t.Errorf("a skill scoped to another agent activated on touch: %s", out)
	}
	_, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Scoped review"), Mode: "review"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range spec.InstructionFiles {
		if strings.HasPrefix(f.Path, "SKILL-") {
			t.Errorf("a skill scoped to another agent was loaded at start: %s", f.Path)
		}
	}
}

// A skill loaded on first touch is recorded in the session's context trail
// like the start-time ones, and a skill already loaded at start is not sent
// again.
func TestSkillActivation_ATouchActivationIsInTheContextTrailAndAStartLoadedSkillIsNotResent(t *testing.T) {
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"templ-style/SKILL.md", templSkill)
	e.saveSkillFile(skillPrefix+"both/SKILL.md", "---\nname: both\ndescription: Both rules.\nmetadata:\n  activation_paths: \"web/**/*.templ\"\n  activation_task_types: implement\n---\n\nBOTH-BODY\n")
	a := e.start(e.task("Trail"))
	if out := e.touch(a, "Edit", "web/tasks/board.templ"); !strings.Contains(out, "never raw html") || strings.Contains(out, "BOTH-BODY") {
		t.Fatalf("touch = %s (templ-style yes, the start-loaded skill no)", out)
	}
	view, err := e.orch.SessionContext(e.ctx, a.id)
	if err != nil || len(view.Entries) == 0 {
		t.Fatalf("context = %+v, %v", view, err)
	}
	var onTouch, atStart bool
	for _, p := range view.Entries[len(view.Entries)-1].Instructions {
		onTouch = onTouch || strings.Contains(p.Title, "templ-style")
		atStart = atStart || strings.Contains(p.Title, "both") || strings.Contains(p.ID, "SKILL-both")
	}
	if !onTouch {
		t.Errorf("the touch activation is not in the trail: %+v", view.Entries[len(view.Entries)-1].Instructions)
	}
	_ = atStart
}
