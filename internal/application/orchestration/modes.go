package orchestration

import (
	"embed"
	"fmt"
	"strings"

	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/domain"
)

// modeFS holds the skills each session mode brings. They ship with the
// server so a mode works without any skill being imported into the catalog.
//
//go:embed modes
var modeFS embed.FS

// modeSkillNames lists, per mode, the skill directories under modes/ it adds.
var modeSkillNames = map[domain.SessionMode][]string{
	domain.SessionModeArchitect: {"task-architecture"},
}

// modeSkills returns the skills a mode adds on top of the session's agent.
func modeSkills(mode domain.SessionMode) ([]domain.Skill, error) {
	names := modeSkillNames[mode]
	out := make([]domain.Skill, 0, len(names))
	for _, name := range names {
		body, err := modeFS.ReadFile("modes/" + name + "/" + skill.FileName)
		if err != nil {
			return nil, fmt.Errorf("mode %s: skill %s: %w", mode, name, err)
		}
		out = append(out, domain.Skill{
			Name:  name,
			Files: []skill.File{{Path: skill.FileName, Content: string(body)}},
		})
	}
	return out, nil
}

// modePrompt is the first message a mode opens the session with. typed is what
// the person wrote, if anything; it follows the mode's own instruction.
func modePrompt(mode domain.SessionMode, tk *domain.Ticket, typed string) string {
	if mode != domain.SessionModeArchitect || tk == nil {
		return typed
	}
	var b strings.Builder
	b.WriteString("Use the task-architecture skill on task " + tk.ID + ": " + tk.Title + "\n")
	if d := strings.TrimSpace(tk.Description); d != "" {
		b.WriteString("\n" + d + "\n")
	}
	b.WriteString("\nIdentify the affected applications, write the contracts and one spec per " +
		"application as task documents, then delegate each spec to a planning agent with " +
		"start_task_session. Do not write any local files.")
	if typed = strings.TrimSpace(typed); typed != "" {
		b.WriteString("\n\n" + typed)
	}
	return b.String()
}
