package orchestration

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
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
// The first one is the skill the mode's first message names.
var modeSkillNames = map[domain.SessionMode][]string{
	domain.SessionModeArchitect: {"task-architecture", "task-grill-with-docs", "task-to-spec"},
	domain.SessionModeImplement: {"task-implement-spec", "task-tdd", "task-code-review"},
	domain.SessionModeDesign:    {"design-artifacts"},
}

// modeSkills returns the skills a mode adds on top of the session's agent.
func modeSkills(mode domain.SessionMode) ([]domain.Skill, error) {
	names := modeSkillNames[mode]
	out := make([]domain.Skill, 0, len(names))
	for _, name := range names {
		files, err := modeSkillFiles(name)
		if err != nil {
			return nil, fmt.Errorf("mode %s: skill %s: %w", mode, name, err)
		}
		out = append(out, domain.Skill{Name: name, Files: files})
	}
	return out, nil
}

// modeSkillFiles reads every file of the skill directory modes/<name>, with
// SKILL.md required, so a skill's reference files ship along with it.
func modeSkillFiles(name string) ([]skill.File, error) {
	root := "modes/" + name
	if _, err := fs.Stat(modeFS, path.Join(root, skill.FileName)); err != nil {
		return nil, err
	}
	var files []skill.File
	err := fs.WalkDir(modeFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := modeFS.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, skill.File{Path: strings.TrimPrefix(p, root+"/"), Content: string(body)})
		return nil
	})
	return files, err
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
		closing = "Work in this order. First grill the person on the design with the task-grill-with-docs skill, " +
			"recording settled terms and decisions as Glossary and ADR files in project/docs/ in your virtual shell, and wait " +
			"for them to confirm you share an understanding. Then identify the affected applications, write the contracts, and " +
			"write one spec per application with the task-to-spec skill, each a file in task/docs/. Then delegate each spec with " +
			"start_task_session in mode implement: the delegate writes tickets, asks you to review them, and implements " +
			"them once you approve. Write documents only in your virtual shell, no other local files.\n\n" +
			"You own this task's status: move it with update_task_status and a reason, based on what your " +
			"delegates report: review when the finished work is ready for the person; the person moves it to " +
			"done after checking it. They report to you; their messages, status checks and the person's review " +
			"answers arrive as turns starting with [task message …] or [status check …]. Answer each one " +
			"(reply_to_session), ask the person for review with request_user_review when something needs " +
			"their eye, and tune loops with set_status_check. When the person adds a resource or a comment to the " +
			"task, every running session, you included, gets a turn starting with [task update · …] naming its file " +
			"in task/resources/ or task/comments/; read it and decide whether a spec, a contract or a delegate's work changes. " +
			"Leave the person a comment for decisions and summaries they should see on the task by writing a file in " +
			"task/comments/; talk to your delegates with task messages, not comments."
	case domain.SessionModeImplement:
		skill = "task-implement-spec"
		closing = "Implement the Spec task document your prompt names, with the Contract documents it depends on. " +
			"First break it into tickets, one file each in task/docs/ in your virtual shell, and ask the architect to review them with " +
			"message_architect (kind review_request, attach the ticket documents). Write no code until the reply's " +
			"verdict is approved; on changes_requested, revise the tickets and ask again. Then implement the ticket " +
			"frontier test first (task-tdd) on your branch, review the result against the spec (task-code-review), and " +
			"commit. Report with message_architect kind status_report at each ticket done and whenever you are blocked, " +
			"and when the spec is done, report done and ask for review naming your branch. Never change a contract you " +
			"consume and never change the task status: the architect owns it."
	case domain.SessionModeDesign:
		skill = "design-artifacts"
		closing = "Make each component or screen a self-contained HTML file and publish it by writing it into " +
			"task/designs/ in your virtual shell; every change there publishes a new revision, so write as soon as it changes. " +
			"Check task/designs/attached.md for the project assets attached to this task and project/designs/ first, " +
			"and build on them (attach_artifact_to_task brings a project asset onto this task); mv an asset to " +
			"project/designs/ once other tasks will reuse it. " +
			"Write images and videos there the same way, and publish a dev server as its loopback url with publish_artifact. " +
			"The person sees each publish in the Design tab of the web UI — tell them so in one line instead of pasting HTML here. " +
			"Reference images, recordings and links the person adds to the task are in task/resources/; " +
			"a turn starting with [task update · …] names the file when they add one or comment. " +
			"Tell the person what you published and what is left in a comment: a file you write in task/comments/."
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
