//go:build !windows

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A session submits the skills it drafted as one proposal (contract "Harness
// server ↔ Agent sessions — instruction-file proposals" §2; ADR 0092).

const (
	featuresSkill = "---\nname: features-harness\ndescription: The features of the harness app.\n---\n\nSee the feature files.\n"
	skillPrefix   = ".claude/skills/"
)

func (e *shellE2E) propose(s runningSession, repository string, skills []string, summary string) (string, bool) {
	e.t.Helper()
	return callTaskRaw(e.t, e.dial(s), "propose_instruction_files", map[string]any{"repository": repository, "skills": skills, "summary": summary})
}

// saveInstruction saves an instruction file of the e2e repository.
func (e *shellE2E) saveSkillFile(path, content string) {
	e.t.Helper()
	if _, err := e.cat.RepositoryArchitectures.SaveInstructionFile(e.ctx, e.repo, path, "skill", 0, content); err != nil {
		e.t.Fatal(err)
	}
}

type submission struct {
	ProposalID string `json:"proposal_id"`
	Files      int    `json:"files"`
	Created    int    `json:"created"`
	Changed    int    `json:"changed"`
	Removed    int    `json:"removed"`
}

// submitted is a task session with drafts for a new skill and for an existing
// one, and the proposal made from them.
func (e *shellE2E) submitted() (s runningSession, got submission) {
	e.t.Helper()
	e.saveSkillFile(skillPrefix+"features-harness/SKILL.md", featuresSkill)
	e.saveSkillFile(skillPrefix+"features-harness/old-feature.md", "stale\n")
	e.saveSkillFile(skillPrefix+"features-harness/kept.md", "kept\n")
	s = e.start(e.task("Verification skill"))
	for rel, content := range map[string]string{
		draftPath("verify-harness", "SKILL.md"):   verifySkill,
		draftPath("verify-harness", "launch.md"):  "# Launch\n",
		draftPath("features-harness", "SKILL.md"): strings.Replace(featuresSkill, "See the feature files.", "See the feature files, all of them.", 1),
		draftPath("features-harness", "kept.md"):  "kept\n",
	} {
		if a := e.writeDraft(s, rel, content); !a.Allow {
			e.t.Fatalf("write %s: %+v", rel, a)
		}
	}
	text, isErr := e.propose(s, "harness", []string{"verify-harness", "features-harness"}, "Verification skill and its feature map")
	if isErr {
		e.t.Fatalf("propose: %s", text)
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		e.t.Fatal(err)
	}
	return s, got
}

func TestProposeInstructionFiles_CountsCreatedChangedAndRemovedFiles(t *testing.T) {
	e := newShellE2E(t)
	_, got := e.submitted()
	// verify-harness: 2 creates. features-harness: SKILL.md changed, kept.md
	// identical (left out), old-feature.md removed.
	if got.ProposalID == "" || got.Files != 4 || got.Created != 2 || got.Changed != 1 || got.Removed != 1 {
		t.Fatalf("submission = %+v", got)
	}
}

func TestProposeInstructionFiles_ASubmittedSkillIsReadOnlyUntilTheProposalIsDecided(t *testing.T) {
	e := newShellE2E(t)
	s, got := e.submitted()

	ans := e.writeDraft(s, draftPath("verify-harness", "SKILL.md"), verifySkill+"more\n")
	if ans.Allow || ans.Code != "PROPOSAL_SUBMITTED" || !strings.Contains(ans.Reason, got.ProposalID) {
		t.Fatalf("a write to a submitted skill = %+v", ans)
	}
	if ans := e.removeDraft(s, draftPath("features-harness", "kept.md")); ans.Allow || ans.Code != "PROPOSAL_SUBMITTED" {
		t.Fatalf("an rm in a submitted skill = %+v", ans)
	}
	eventually(t, "the index shows the draft read-only", func() error {
		idx, err := shellIndex(s.shell)
		if err != nil {
			return err
		}
		if row := idx[draftPath("verify-harness", "SKILL.md")]; row.Kind != "instruction-draft" || row.Writable {
			return &stringError{"row = " + row.Path + " writable"}
		}
		return nil
	})
	if text, isErr := e.propose(s, "harness", []string{"verify-harness"}, "Again"); !isErr || !strings.Contains(text, "PROPOSAL_SUBMITTED") {
		t.Fatalf("a second submit = %q (error %v)", text, isErr)
	}
	// A skill that is not in the proposal stays writable.
	if ans := e.writeDraft(s, draftPath("other-skill", "SKILL.md"), verifySkill); !ans.Allow {
		t.Fatalf("a write to another skill = %+v", ans)
	}
}

func TestProposeInstructionFiles_RefusalsNameTheirCode(t *testing.T) {
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"same/SKILL.md", featuresSkill)
	s := e.start(e.task("Verification skill"))
	for rel, content := range map[string]string{
		draftPath("same", "SKILL.md"):        featuresSkill,
		draftPath("no-skill-md", "notes.md"): "x\n",
		draftPath("no-front", "SKILL.md"):    "# no frontmatter\n",
		draftPath("no-desc", "SKILL.md"):     "---\nname: no-desc\n---\n",
		draftPath("fine", "SKILL.md"):        verifySkill,
	} {
		if a := e.writeDraft(s, rel, content); !a.Allow {
			t.Fatalf("write %s: %+v", rel, a)
		}
	}
	cases := []struct {
		name, repository string
		skills           []string
		summary, code    string
	}{
		{"no skills", "harness", nil, "s", "INVALID_INPUT"},
		{"no summary", "harness", []string{"fine"}, " ", "INVALID_INPUT"},
		{"a skill with no draft", "harness", []string{"ghost"}, "s", "INVALID_INPUT"},
		{"an unknown repository", "elsewhere", []string{"fine"}, "s", "REPOSITORY_NOT_FOUND"},
		{"no SKILL.md", "harness", []string{"no-skill-md"}, "s", "INSTRUCTION_INVALID"},
		{"no frontmatter", "harness", []string{"no-front"}, "s", "INSTRUCTION_INVALID"},
		{"no description", "harness", []string{"no-desc"}, "s", "INSTRUCTION_INVALID"},
		{"a draft equal to the saved skill", "harness", []string{"same"}, "s", "PROPOSAL_EMPTY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, isErr := e.propose(s, c.repository, c.skills, c.summary)
			if !isErr || !strings.Contains(text, c.code) {
				t.Fatalf("answer = %q (error %v), want %s", text, isErr, c.code)
			}
		})
	}
}

func TestProposeInstructionFiles_ASubmitIsOnTheFeedAndInTheInbox(t *testing.T) {
	e := newShellE2E(t)
	feed := &archE2E{t: t, ctx: e.ctx, srv: e.srv, project: e.project}
	feed.follow()
	s, got := e.submitted()

	feed.sawOnFeed("instruction_proposal_changed", `"repository_id":"`+e.repo+`"`, `"proposal_id":"`+got.ProposalID+`"`, `"state":"open"`, `"open_count":1`)

	page, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{ProjectID: e.project})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range page.Notifications {
		if n.Level == domain.NotificationAction && n.SessionID == s.id && n.Title == "Instruction files proposed: verify-harness, features-harness in harness" &&
			n.Body == "Verification skill and its feature map" && strings.Contains(n.Link, e.repo) && strings.Contains(n.Link, got.ProposalID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no action notification for the proposal: %+v", page.Notifications)
	}
}

// A skill's activation rule is checked beside its name and description.
func TestProposeInstructionFiles_ASkillWithAnInvalidActivationRuleIsRefused(t *testing.T) {
	e := newShellE2E(t)
	s := e.start(e.task("Activation rules"))
	skill := func(metadata string) string {
		return "---\nname: gated\ndescription: A skill with a rule.\nmetadata:\n" + metadata + "---\n\nBody.\n"
	}
	for rel, content := range map[string]string{
		draftPath("bad-glob", "SKILL.md"):   skill("  activation_paths: \"web/[a-\"\n"),
		draftPath("bad-type", "SKILL.md"):   skill("  activation_task_types: deploy\n"),
		draftPath("good-rule", "SKILL.md"):  skill("  activation_paths: \"web/**/*.templ\"\n  activation_task_types: implement\n"),
		draftPath("list-paths", "SKILL.md"): skill("  activation_paths: [web/**, a/*.go]\n"),
		draftPath("list-types", "SKILL.md"): skill("  activation_task_types:\n    - review\n"),
		draftPath("bad-yaml", "SKILL.md"):   skill("  activation_paths: **/*.templ\n"),
	} {
		if a := e.writeDraft(s, rel, content); !a.Allow {
			t.Fatalf("write %s: %+v", rel, a)
		}
	}
	for skillName, field := range map[string]string{"bad-glob": "activation_paths", "bad-type": "deploy", "list-paths": "activation_paths must be one string", "list-types": "activation_task_types must be one string", "bad-yaml": "front matter is not valid YAML"} {
		text, isErr := e.propose(s, "harness", []string{skillName}, "s")
		if !isErr || !strings.Contains(text, "INSTRUCTION_INVALID") || !strings.Contains(text, field) {
			t.Fatalf("%s: answer = %q (error %v)", skillName, text, isErr)
		}
	}
	if text, isErr := e.propose(s, "harness", []string{"good-rule"}, "A rule"); isErr {
		t.Fatalf("a valid rule was refused: %s", text)
	}
}
