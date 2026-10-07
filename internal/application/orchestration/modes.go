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
	domain.SessionModeDesign:    {"design-artifacts"},
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
	if tk == nil {
		return typed
	}
	var skill, closing string
	switch mode {
	case domain.SessionModeArchitect:
		skill = "task-architecture"
		closing = "Identify the affected applications, write the contracts and one spec per " +
			"application as task documents, then delegate each spec to a planning agent with " +
			"start_task_session. Do not write any local files."
	case domain.SessionModeDesign:
		skill = "design-artifacts"
		closing = "Work under design/ in this worktree. Make each component or screen a self-contained HTML " +
			"file and publish every result with publish_artifact as soon as it changes, with a one-line note; " +
			"check list_project_artifacts first and build on the project's assets, and move an asset to project level " +
			"with move_artifact_to_project once other tasks will reuse it; " +
			"publish images and videos the same way, and a dev server as its loopback url. The person sees " +
			"each publish in the Design tab of the web UI — tell them so in one line instead of pasting HTML here."
	default:
		return typed
	}
	var b strings.Builder
	b.WriteString("Use the " + skill + " skill on task " + tk.ID + ": " + tk.Title + "\n")
	if d := strings.TrimSpace(tk.Description); d != "" {
		b.WriteString("\n" + d + "\n")
	}
	b.WriteString("\n" + closing)
	if typed = strings.TrimSpace(typed); typed != "" {
		b.WriteString("\n\n" + typed)
	}
	return b.String()
}
