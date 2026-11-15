package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// InstructionDraftsDir is the task-scoped folder of a task's virtual shell
// where sessions draft skills for a repository (ADR 0092).
const InstructionDraftsDir = "task/instruction-drafts"

var instructionSkillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// InstructionDraftPath is a path under InstructionDraftsDir split into the
// repository it is for, the skill, and the file inside the skill.
type InstructionDraftPath struct {
	Repository string
	Skill      string
	// File is the path inside the skill's folder, with "/" separators.
	File string
}

// InstructionPath is the repository path the draft file becomes.
func (p InstructionDraftPath) InstructionPath() string {
	return ".claude/skills/" + p.Skill + "/" + p.File
}

// ParseInstructionDraftPath splits rel, a path below InstructionDraftsDir
// (<repository>/<skill>/<file>), and lists what stops it from becoming a
// skill file: its shape, the skill's name, and a path that would map to a
// memory file or a sub-agent. The repository is not checked here.
func ParseInstructionDraftPath(rel string) (InstructionDraftPath, []string) {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		return InstructionDraftPath{}, []string{fmt.Sprintf("%s/%s: a draft file is %s/<repository>/<skill>/<file>", InstructionDraftsDir, rel, InstructionDraftsDir)}
	}
	p := InstructionDraftPath{Repository: parts[0], Skill: parts[1], File: strings.Join(parts[2:], "/")}
	var problems []string
	if p.Repository == "" {
		problems = append(problems, "the repository folder has no name")
	}
	if !instructionSkillName.MatchString(p.Skill) {
		problems = append(problems, fmt.Sprintf("%q is not a skill name: use lower-case letters, digits and single hyphens", p.Skill))
	}
	if problem := InstructionPathProblem(p.File); problem != "" {
		problems = append(problems, p.File+": "+problem)
	} else if kind := InstructionKindOf(p.InstructionPath()); kind != "skill" {
		problems = append(problems, fmt.Sprintf("%s would be a %s file: a draft holds skill files only", p.File, kind))
	}
	return p, problems
}
