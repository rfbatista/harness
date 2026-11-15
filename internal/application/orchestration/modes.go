package orchestration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra/builtinskills"
	"operators-mcp/internal/ports"
)

// modeSkills returns the skills a mode adds on top of the session's agent,
// from the server's built-in set.
func modeSkills(mode domain.SessionMode) ([]domain.Skill, error) {
	if err := builtinskills.Err(); err != nil {
		return nil, fmt.Errorf("mode %s: %w", mode, err)
	}
	return domainSkills(builtinskills.ForMode(mode)), nil
}

// sessionSkills is what a session's plugin dir holds: the agent's skills, then
// the mode's. A skill the agent names that is built in comes from the embedded
// set, so a catalog skill of the same name never wins, and one the mode also
// carries is kept once, as the mode's copy (ADR 0088).
func sessionSkills(agent *domain.Agent, mode []domain.Skill) []domain.Skill {
	inMode := map[string]bool{}
	for _, s := range mode {
		inMode[domain.Slug(s.Name)] = true
	}
	var out []domain.Skill
	have := map[string]bool{}
	add := func(s domain.Skill) {
		slug := domain.Slug(s.Name)
		if b, ok := builtinskills.Get(slug); ok {
			s = domainSkills([]builtinskills.Skill{b})[0]
		}
		if inMode[slug] || have[slug] {
			return
		}
		have[slug] = true
		out = append(out, s)
	}
	if agent != nil {
		for _, s := range agent.Skills {
			add(s)
		}
		for _, id := range agent.SkillIDs {
			if _, ok := builtinskills.Get(id); ok {
				add(domain.Skill{Name: id})
			}
		}
	}
	return append(out, mode...)
}

func domainSkills(in []builtinskills.Skill) []domain.Skill {
	out := make([]domain.Skill, 0, len(in))
	for _, s := range in {
		out = append(out, domain.Skill{Name: s.Name, Files: s.Files})
	}
	return out
}

// modePrompt is the first message a mode opens the session with. typed is what
// the person wrote, if anything; it follows the mode's own instruction. With
// no task, only a product-mode session gets an opening, on its project; any
// other mode's first message is what the person typed. A verification mode
// reports to the architect when the session is its delegate, and sums up in a
// task comment otherwise.
func modePrompt(mode domain.SessionMode, tk *domain.Ticket, project *domain.Project, role domain.SessionRole, typed string) string {
	if tk == nil {
		if !mode.NeedsTask() && project != nil {
			return projectPrompt(project, typed)
		}
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
	case domain.SessionModeProduct:
		skill = "task-product"
		closing = "Work in this order. First interview the person on the feature in rounds of numbered questions, each with " +
			"your recommended answer, until they confirm its goal, its users, its scope in and out, and its risks; look facts up " +
			"yourself and ask them only for decisions. Then write the PRD as a file titled \"PRD: <feature>\" in task/docs/ of your " +
			"virtual shell, following PRD-FORMAT.md. Then create one child task per piece of work with create_task (the parent " +
			"defaults to this task) and write one brief per child as a file in tasks/<id>/docs/, following TASK-BRIEF-FORMAT.md: " +
			"what to build, acceptance criteria, boundaries touched, out of scope. Then ask the person for review with " +
			"request_user_review, attaching the PRD (this task's document, its id in .index.json) and naming each child task " +
			"by id and title in the body: only this task's documents attach, and each brief is on its child. Write documents " +
			"only in your virtual shell, no other local files.\n\n" +
			"Never write code, and never start implementation sessions: the person or an architect decides who implements each " +
			"child and when. You may move this task's status with update_task_status and a reason: review when you ask for the " +
			"review, back to in_progress when they request changes. When the person adds a resource or a comment to the task, " +
			"a turn starting with [task update \u00b7 \u2026] names its file in task/resources/ or task/comments/; read it and decide whether the " +
			"scope changes. Leave the person a comment for the summary and the decisions they should see on the task by " +
			"writing a file in task/comments/."
	case domain.SessionModeReview:
		skill = "task-review"
		closing = "Read the diff of your branch against the branch it was cut from (`git diff <base>...HEAD` in your worktree; " +
			"branches are shared), or the pull request your prompt names. Write one document titled `Review: <branch>` in " +
			"task/docs/: a summary of the change, the findings, and the verdict `approve` or `request changes` on its own line. " +
			"Write no code and change no file in the repository. " +
			verificationReport(role, "the verdict in the text and the document attached", "the verdict and the document") +
			" Never change the task status."
	case domain.SessionModeTest:
		skill = "task-test"
		closing = "Read the diff of your branch against the branch it was cut from. Write tests for it on your branch, test first " +
			"with task-tdd, at the seams the spec names (read the Spec document in task/docs/ when there is one), run them, and commit. " +
			"Write one document titled `Test report: <branch>` in task/docs/: the tests written, the results, and what the change " +
			"left untested. Change no production code; a failing test that shows a defect stays failing and is named in the report. " +
			verificationReport(role, "your branch named and the document attached", "your branch and the document") +
			" Never change the task status."
	case domain.SessionModeQA:
		skill = "task-qa"
		closing = "Walk each use case your prompt lists (or the user stories of the Spec documents in task/docs/ when it lists none) " +
			"in the running application, through the browser tools, at the service URLs your prompt names (`list_services` shows them); " +
			"start nothing yourself unless your prompt says you may. Publish at least one screenshot per use case into task/designs/ " +
			"(a folder per use case is fine) and a video per flow where you can record one; name the files after the use case. " +
			"Write one document titled `QA: <task title>` in task/docs/: one entry per use case with pass or fail, what you saw and " +
			"its artifacts. Write no code. " +
			verificationReport(role, "the document and the artifacts attached (ids in .index.json)", "the document and the artifacts") +
			" Never change the task status."
	case domain.SessionModeOperate:
		skill = "task-operate"
		closing = "Run the task's code for the sessions that test it. Use the services tools: every create, update, start, restart " +
			"and stop is a service request the person approves; give a `reason` each time. Run the task's branch " +
			"(`worktree: \"branch:<name>\"`) unless your prompt names another place. Set the variables the tests need as run overrides, " +
			"never in the repository. After each start or restart, when the architect asks, and before you report done, check the " +
			"service: its status, the tail of its log, and a request to the URL your prompt names or the service's port; a service that " +
			"exited or failed is a status_report with status blocked at once (a task comment when you have no architect), with the " +
			"log's decisive line. When your prompt carries the person's request to seed and run the task's application, follow " +
			"task-run-app: write the results, the test credentials included, into the Seed document in task/docs/ and write no " +
			"task comment with them. " +
			verificationReport(role, operateResult+", and for a seeded run the Seed document attached", operateResult+", and for a seeded run the Seed document named, without the credentials") +
			" Write no code. Never change the task status."
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

// operateResult is what an Operator reports, to its architect or in a comment.
const operateResult = "the service names, where each runs (branch and commit) and its URLs"

// verificationReport is the line of a verification mode's opening that says
// how it reports: a delegate to its architect, a peer in a task comment.
func verificationReport(role domain.SessionRole, delegate, peer string) string {
	if role == domain.RoleDelegate {
		return "Report with message_architect kind status_report, status done, with " + delegate + "."
	}
	return "When you are done, sum up in a task comment (a file in task/comments/) with " + peer + "; no architect reads your report."
}

// projectPrompt opens a product-mode session that has no task: the same
// interview as on a task, then the feature task is created first and the
// PRD and the briefs land under tasks/<id>/docs/ (ADR 0016).
func projectPrompt(project *domain.Project, typed string) string {
	var b strings.Builder
	b.WriteString("Use the task-product skill on project " + project.Name + ", which has no task yet for the feature you will define.\n")
	b.WriteString("\nWork in this order. First interview the person on the feature in rounds of numbered questions, each with " +
		"your recommended answer, until they confirm its goal, its users, its scope in and out, and its risks; look facts up " +
		"yourself and ask them only for decisions. Then create the feature task with create_task, passing parent_id \"\" " +
		"explicitly so it is a top-level task, its title the feature's name and its description the goal in two sentences; " +
		"note its id. Then write the PRD as a file titled \"PRD: <feature>\" in tasks/<id>/docs/ of that task in your " +
		"virtual shell, following PRD-FORMAT.md. Then create one child task per piece of work with create_task, parent_id " +
		"the feature task's id (there is no session task to default to), and write one brief per child as a file in " +
		"tasks/<id>/docs/, following TASK-BRIEF-FORMAT.md: what to build, acceptance criteria, boundaries touched, out of " +
		"scope. This session has no task of its own, so there is no task/ folder in your shell and the tools that act on " +
		"\"this task\" (get_task, start_task_session, list_task_sessions) answer SESSION_HAS_NO_TASK; request_user_review " +
		"and update_task_status do too unless you pass task_id. When the PRD and the briefs are written, ask the person for " +
		"review with request_user_review and task_id the feature task's id, attaching the PRD (the feature task's document, " +
		"its id in .index.json) and naming each child task by id and title in the body; then move the feature task to review " +
		"with update_task_status, task_id the feature task's id, and a reason. If the review cannot be opened, tell the person " +
		"with notify (level action, link the feature task's page) instead. Sum up what you created in your last message. " +
		"Write documents only in your virtual shell, no other local files.\n\n" +
		"Never write code, and never start implementation sessions: the person or an architect decides who implements each " +
		"child and when.")
	if typed = strings.TrimSpace(typed); typed != "" {
		b.WriteString("\n\n" + typed)
	}
	return b.String()
}

// planBringUnder judges the sessions a person selected for the architect
// about to start on ticket: only an architect-mode start takes a selection.
func (s *Service) planBringUnder(ctx context.Context, mode domain.SessionMode, ticket *domain.Ticket, ids []string) (*ports.BringUnderPlan, error) {
	if mode != domain.SessionModeArchitect || ticket == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "INVALID_INPUT: bring_under_session_ids needs mode architect on a task"}
	}
	if s.BringUnder == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "UNAVAILABLE: the architect channel is not configured"}
	}
	return s.BringUnder.PlanBringUnder(ctx, ticket.ID, ids)
}

// architectOverPrompt opens an architect started over sessions the person
// brought under it: the work is underway, so it supervises instead of
// grilling and writing specs (see "Contract: Harness server ↔ Agent sessions —
// turns when sessions are brought under", §3). The typed prompt follows.
func (s *Service) architectOverPrompt(ctx context.Context, tk *domain.Ticket, delegates []*domain.Session, typed string) string {
	var b strings.Builder
	b.WriteString("Use the task-architecture skill on task " + tk.ID + ": " + tk.Title + "\n")
	if d := strings.TrimSpace(tk.Description); d != "" {
		b.WriteString("\n" + d + "\n")
	}
	b.WriteString("\nYou were started to supervise sessions the person brought under you; the work is already underway, so do not grill the person on a design and do not write specs or start new delegates unless they ask. Your delegates:\n\n")
	now := time.Now()
	for _, d := range delegates {
		name := s.agentName(ctx, d.AgentID)
		if name == "" {
			name = "claude"
		}
		b.WriteString(domain.BroughtUnderLine(name, d, now) + "\n")
	}
	b.WriteString("\nStart by reading list_task_sessions and list_task_messages. Supervise them: answer what they report with reply_to_session, tune or pause each loop with set_status_check (the running ones have one at the default interval), ask the person for review with request_user_review when their work is ready, and move the task status with update_task_status and a reason as the work moves; the person moves it to done. When the person adds a resource or a comment to the task, a turn starting with [task update · …] names its file; read it. Leave the person a comment for decisions and summaries they should see by writing a file in task/comments/; talk to your delegates with task messages, not comments.")
	if typed = strings.TrimSpace(typed); typed != "" {
		b.WriteString("\n\n" + typed)
	}
	return b.String()
}
