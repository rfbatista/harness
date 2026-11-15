//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The instruction draft in the virtual shell: task/instruction-drafts/
// <repository>/<skill>/… (contract "Harness server ↔ Agent sessions —
// instruction-file proposals" §1; ADR 0092).

const draftsDir = "task/instruction-drafts"

// draftPath is the shell path of a draft file of the e2e repository.
func draftPath(skill, rel string) string { return draftsDir + "/harness/" + skill + "/" + rel }

// writeDraft is what the edit proxy does for a write at rel: check, the file
// is written, commit. It answers the commit's answer, or the check's when
// that refused.
func (e *shellE2E) writeDraft(s runningSession, rel, content string) shellAnswer {
	e.t.Helper()
	if a, _ := e.shellCall(s, "check", "write", rel); !a.Allow {
		return a
	}
	writeShellFile(e.t, s, rel, content)
	a, _ := e.shellCall(s, "commit", "write", rel)
	return a
}

func (e *shellE2E) removeDraft(s runningSession, rel string) shellAnswer {
	e.t.Helper()
	if a, _ := e.shellCall(s, "check", "rm", rel); !a.Allow {
		return a
	}
	removeShellFile(e.t, s, rel)
	a, _ := e.shellCall(s, "commit", "rm", rel)
	return a
}

const verifySkill = "---\nname: verify-harness\ndescription: Verify the harness app by driving it.\n---\n\nRun the app, then drive it.\n"

func TestInstructionDraft_ASessionWritesASkillAndAnotherSessionOnTheTaskReadsIt(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Verification skill")
	a, b := e.start(task), e.start(task)

	if _, err := os.Stat(filepath.Join(a.shell, draftsDir)); !os.IsNotExist(err) {
		t.Fatalf("the draft folder is there before the first write: %v", err)
	}
	if ans := e.writeDraft(a, draftPath("verify-harness", "SKILL.md"), verifySkill); !ans.Allow {
		t.Fatalf("write SKILL.md: %+v", ans)
	}
	if ans := e.writeDraft(a, draftPath("verify-harness", "reference/launch.md"), "# Launch\n"); !ans.Allow {
		t.Fatalf("write a reference file: %+v", ans)
	}

	eventually(t, "the other session reads the draft", func() error {
		return fileWith(b.shell, draftPath("verify-harness", "SKILL.md"), "name: verify-harness")
	})
	for _, s := range []runningSession{a, b} {
		eventually(t, "the index lists the draft file", func() error {
			idx, err := shellIndex(s.shell)
			if err != nil {
				return err
			}
			row, ok := idx[draftPath("verify-harness", "reference/launch.md")]
			if !ok || row.Kind != "instruction-draft" || row.Scope != "task" || row.Repository != "harness" || row.Skill != "verify-harness" || !row.Writable {
				return fmt.Errorf("index row = %+v (found %v)", row, ok)
			}
			return nil
		})
	}
	if err := fileWith(a.shell, "README.md", "task/instruction-drafts/"); err != nil {
		t.Errorf("the README does not describe the folder: %v", err)
	}

	// a is about to write when b changes the file: a's write is stale.
	skill := draftPath("verify-harness", "SKILL.md")
	if ans, raw := e.shellCall(a, "check", "write", skill); !ans.Allow {
		t.Fatalf("check refused: %s", raw)
	}
	changed := strings.Replace(verifySkill, "drive it.", "drive it twice.", 1)
	eventually(t, "b changes the file", func() error {
		if ans := e.writeDraft(b, skill, changed); !ans.Allow {
			return fmt.Errorf("%+v", ans)
		}
		return nil
	})
	writeShellFile(t, a, skill, verifySkill+"more\n")
	if ans, _ := e.shellCall(a, "commit", "write", skill); ans.Allow || ans.Code != "BASE_VERSION_STALE" {
		t.Fatalf("a stale write = %+v", ans)
	}
	eventually(t, "a reads b's change", func() error { return fileWith(a.shell, skill, "drive it twice.") })
}

func TestInstructionDraft_AWriteThatCouldNeverBeASkillFileIsRefusedAtOnce(t *testing.T) {
	e := newShellE2E(t)
	s := e.start(e.task("Verification skill"))

	cases := map[string]struct{ rel, content string }{
		"a repository not in the project": {draftsDir + "/elsewhere/verify/SKILL.md", verifySkill},
		"a skill name that is no slug":    {draftPath("Verify_Harness", "SKILL.md"), verifySkill},
		"a file directly in the folder":   {draftsDir + "/notes.md", "x"},
		"a file directly in a repository": {draftsDir + "/harness/notes.md", "x"},
		"a file over 256 KB":              {draftPath("verify-harness", "big.md"), strings.Repeat("x", 256*1024+1)},
		"a memory file inside a skill":    {draftPath("verify-harness", "CLAUDE.md"), "rules"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ans := e.writeDraft(s, c.rel, c.content)
			if ans.Allow || ans.Code != "INSTRUCTION_INVALID" {
				t.Fatalf("answer = %+v, want INSTRUCTION_INVALID", ans)
			}
		})
	}
	if idx, err := shellIndex(s.shell); err != nil {
		t.Fatal(err)
	} else {
		for p := range idx {
			if strings.HasPrefix(p, draftsDir) {
				t.Errorf("a refused write is indexed: %s", p)
			}
		}
	}
}

func TestInstructionDraft_ASessionRemovesADraftFile(t *testing.T) {
	e := newShellE2E(t)
	a := e.start(e.task("Verification skill"))
	rel := draftPath("verify-harness", "SKILL.md")
	if ans := e.writeDraft(a, rel, verifySkill); !ans.Allow {
		t.Fatal(ans)
	}
	if ans := e.removeDraft(a, rel); !ans.Allow {
		t.Fatalf("rm: %+v", ans)
	}
	b := e.start(e.task("Another task"))
	if _, err := os.Stat(filepath.Join(b.shell, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("another task's shell has the draft: %v", err)
	}
	idx, _ := shellIndex(a.shell)
	if _, ok := idx[rel]; ok {
		t.Error("a removed draft file is still indexed")
	}
}
