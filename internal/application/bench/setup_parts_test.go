package bench

import (
	"reflect"
	"testing"

	"operators-mcp/internal/domain"
)

func TestASetupRecordCountsTheAgentSkillsAndInstructionFiles(t *testing.T) {
	rec := SetupRecord(baseSetup())
	if rec.Agents != 1 || rec.Skills != 1 || rec.Instructions != 2 {
		t.Fatalf("counts = agents %d, skills %d, instructions %d", rec.Agents, rec.Skills, rec.Instructions)
	}
	if rec.Fingerprint != Fingerprint(baseSetup()) || rec.Model != "sonnet" || rec.JudgeModel != "opus" || rec.CLIVersion != "2.1.0" {
		t.Fatalf("record = %+v", rec)
	}
	for _, part := range []string{"agent", "model", "judge model", "claude cli", "skill skills/tdd/SKILL.md", "instruction CLAUDE.md", "instruction ARCHITECTURE.md"} {
		if rec.Parts[part] == "" {
			t.Fatalf("part %q is not recorded: %v", part, rec.Parts)
		}
	}
}

func TestChangedPartsNameEachPartWhoseHashMoved(t *testing.T) {
	prev := baseSetup()
	cur := baseSetup()
	cur.Instructions[0].Content = "be verbose" // CLAUDE.md
	cur.Skills = append(cur.Skills, SetupFile{Path: "skills/verify/SKILL.md", Content: "check"})
	cur.Model = "opus"
	cur.Instructions = cur.Instructions[:1] // ARCHITECTURE.md is gone

	a := domain.Scorecard{Setup: SetupRecord(prev)}
	b := domain.Scorecard{Setup: SetupRecord(cur)}
	got := Compare(a, b).ChangedParts
	want := []string{"instruction ARCHITECTURE.md", "instruction CLAUDE.md", "model", "skill skills/verify/SKILL.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed parts = %v, want %v", got, want)
	}
}

func TestAnUnchangedSetupChangesNoParts(t *testing.T) {
	a := domain.Scorecard{Setup: SetupRecord(baseSetup())}
	b := domain.Scorecard{Setup: SetupRecord(baseSetup())}
	if got := Compare(a, b).ChangedParts; got == nil || len(got) != 0 {
		t.Fatalf("changed parts = %#v, want an empty list", got)
	}
}

func TestAScorecardWithoutPartsCannotNameWhatChanged(t *testing.T) {
	old := domain.Scorecard{Setup: domain.BenchSetupRecord{Fingerprint: "f1"}} // recorded before parts existed
	cur := domain.Scorecard{Setup: SetupRecord(baseSetup())}
	if got := Compare(old, cur).ChangedParts; got != nil {
		t.Fatalf("changed parts = %v, want null (not recorded)", got)
	}
	if got := Compare(cur, old).ChangedParts; got != nil {
		t.Fatalf("changed parts the other way = %v, want null", got)
	}
}
