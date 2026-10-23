package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// taskServerName is the MCP server exposing the task the session was spawned
// into and the other sessions working on it. The literal matters: taskBrief names its
// tools as mcp__task__* in the system prompt, and the two have to agree.
const taskServerName = "task"

// applyTaskContext gives a session spawned into a task everything it needs to
// work on it: the per-session task MCP server, an allow-list so reading and
// writing its own task never stops for an approval, and a short brief appended
// to the system prompt so the agent knows about the task without a tool call.
// Document bodies stay in the session's virtual shell, so the prompt stays
// small.
//
// taskServerURL is empty when the host serves no per-session task endpoint; the
// brief and the allow-list still apply, only the server entry is skipped.
//
// role is what the session is on its task: a delegate is told to report to the
// architect instead of moving the status, the architect that it owns the
// status and how its delegates reach it; a peer gets the brief unchanged.
//
// counts are the task's resources and comments, for the brief; the zero value
// leaves the counts out.
func applyTaskContext(cfg *llmkit.SessionConfig, tk *domain.Ticket, taskServerURL string, role domain.SessionRole, counts taskCounts) {
	if tk == nil {
		return
	}
	if taskServerURL != "" {
		cfg.MCPServers = append(cfg.MCPServers, llmkit.MCPServerSpec{
			Name:      taskServerName,
			Transport: "http",
			URL:       taskServerURL,
		})
	}
	for _, name := range tooling.SessionTaskToolNames {
		cfg.AllowedTools = append(cfg.AllowedTools, llmkit.MCPToolRef(taskServerName, name))
	}
	cfg.AppendSystem = appendSection(cfg.AppendSystem, taskBrief(tk, role, counts))
}

// taskBrief is the block describing the task, the virtual shell holding
// its resources and the tools that reach the rest, then what the person
// added to the task. Only the paragraph about the
// task's status depends on role.
func taskBrief(tk *domain.Ticket, role domain.SessionRole, counts taskCounts) string {
	var b strings.Builder
	b.WriteString("## Your task\n\n")
	b.WriteString("You are working on task " + tk.ID + ": " + tk.Title + "\n")
	if tk.Status != "" {
		b.WriteString("Status: " + string(tk.Status) + "\n")
	}
	if tk.Description != "" {
		b.WriteString("\n" + tk.Description + "\n")
	}
	b.WriteString(`
Your task's and your project's documents, comments, resources and designs
are files in your virtual shell, $HARNESS_SHELL ("Your virtual shell" below
names its path; its README.md maps the layout and the rules). Work on them
with your ordinary tools; none of it takes a tool call:

- Read with Read, grep, ls and sed -n. .index.json lists every file with its id, kind, scope and version.
- Write with Write, Edit, sed -i, tee, cp, mv, rm, touch and mkdir. Each change is checked before it lands and stored once it has. Shell redirects (>, >>) into the folder are refused: they would not be stored.

What the folders hold:

- task/docs/ — this task's documents. A file you create there is a new document linked to this task, titled by its first heading. Any text format: .md, .html, .txt.
- project/docs/ — the project's documents: architecture, conventions, decisions, the specs and contracts other tasks build on. ` + moveDocsLine(counts.agentsMove) + ` mv one from project/docs/ back to task/docs/ to make it this task's again.
- task/comments/ — the task's comments, one file each (see "Resources and comments").
- task/resources/ — what the person added: uploads under their names, links in links.md. Read-only.
- task/designs/ — what this task published to the web UI's Design tab. A file you create there is published, a change publishes a new revision, rm unpublishes it.
- project/designs/ — the project's design assets, from any task: a logo, a palette, a component, a reference screen. Look here before you make a new asset. mv a design from task/designs/ to project/designs/ once other tasks will reuse it; the harness keeps its own copy, so it outlives this session.

task/task.json is this task; README.md, .index.json and the urls.md,
attached.md and links.md lists are read-only.

Files can be large. grep for what you need, read that range with sed -n or
Read with offset and limit, never the whole file; change a part with Edit
or sed -i rather than rewriting it.

A refused write says why, starting with its code:

- BASE_VERSION_STALE — the file changed since you got it and has been refreshed: read it again and redo the change
- READ_ONLY, COMMENT_NOT_YOURS — the path is not yours to change
- REDIRECT_DENIED — write with Write, Edit or tee instead of > or >>
- NOT_ON_TASK — only a document linked to this task can move to task/docs/
- PROJECT_MOVE_DENIED — ` + moveDeniedLine(counts.agentsMove) + `
- NOT_STORED — the path is outside the folders the harness stores; nothing was written
- SHELL_UNAVAILABLE — the harness could not be reached: redo the change once it is back

Your task itself:

- mcp__task__get_task — this task: title, description, status and its architect

` + statusSection(role) + `

Other agents may be working on this task at the same time, each in its own
session, branch and worktree. Check before starting, and before touching
shared files, so you do not duplicate or undo their work:

- mcp__task__list_task_sessions — the other sessions on this task: agent, brief, status, last action, branch, worktree
- mcp__task__start_task_session — start another session on this task with a prompt (optionally an agent, a repository, a base branch), to hand off or parallelise a well-separated part of the work

To see the project around the task, and who to hand work to:

- mcp__task__list_project_repositories — the project's repositories: its applications
- mcp__task__list_bounded_contexts — its bounded contexts, with their language and zones
- mcp__task__list_agents — the agents a session can run as, and what each is for

The project's tasks, to shape the work around this one:

- mcp__task__list_project_tasks — the project's tasks: id, title, status, parent_id; with parent_id, the children of one
- mcp__task__create_task — create a task in this project; parent_id defaults to this task, "" makes a top-level task

A dev server you started shows in the Design tab too (files go in task/designs/):

- mcp__task__publish_artifact — publish a dev server's loopback URL; publish it again to refresh it

A project asset can be attached to this task, by a person or by you, so it
shows in this task's Design tab beside what the task produced, and in
task/designs/attached.md. Its artifact_id is its id in .index.json:

- mcp__task__attach_artifact_to_task — attach a project asset, from any task of this project, to this task
- mcp__task__detach_artifact_from_task — detach one from this task; it stays in the project

These tools always act on this task; they take no project or task id.`)
	return b.String() + "\n\n" + resourcesSection(counts) + "\n\n" + notificationsSection
}

// moveDocsLine tells a session on a task who moves its documents to project
// level, by the value of "Agents move documents to project" when it started
// (ADR 0020).
func moveDocsLine(agentsMove bool) string {
	if agentsMove {
		return `The setting "Agents move documents to project" was on when this session started: once a document in task/docs/ says something the next task needs, not just this one, mv it to project/docs/ yourself, with no approval and no notification. Only this task's own documents move.`
	}
	return `The setting "Agents move documents to project" was off when this session started, so a person moves documents to project level: once one in task/docs/ says something the next task needs, not just this one, ask them to move it, in a task comment or a message to your architect.`
}

// moveDeniedLine is the brief's PROJECT_MOVE_DENIED entry for the setting.
func moveDeniedLine(agentsMove bool) string {
	if agentsMove {
		return `a document cannot move to project/docs/: only your own task's documents move, and only while "Agents move documents to project" is on; if it was turned off, ask the person to move it`
	}
	return `a document cannot move to project/docs/: ask the person to move it, or to turn on "Agents move documents to project"`
}

// projectMoveLine is the project brief's part on moving documents: this
// session has no task, so it moves none itself.
func projectMoveLine(agentsMove bool) string {
	if agentsMove {
		return `The setting "Agents move documents to project" was on when this session started: a session on a task moves that task's documents here itself, but this session has no task, so write yours onto a task.`
	}
	return `The setting "Agents move documents to project" was off when this session started: a person moves documents to project level, so write yours onto a task.`
}

// notificationsSection names the inbox tools and what notify is for (see
// "Contract: Harness server ↔ Agent sessions — notification tools" §4).
const notificationsSection = `The person has one inbox across every project: review requests, sessions
waiting for approval, tasks in review, failed sessions, and what agents post.
You can read it and post to it; reading never marks anything read:

- mcp__task__list_notifications — the inbox, newest first: scope all (default) or project, unread_only (default true), since, limit
- mcp__task__notify — post to it as this session: title, body, level info or action, link (default: this task's page); it shows in the web UI live and goes out through every enabled delivery provider

Use notify only for what the person has to know or do, never for progress
chatter: request_user_review and moving a task to review already notify the
person. A session may post at most 20 an hour.`

// shellSection names the session's virtual shell, the folder holding its
// task's and project's resources as files; its README.md maps the layout.
func shellSection(dir string) string {
	return "## Your virtual shell\n\n" +
		"Your task's and project's documents, comments, resources and designs are files in\n" +
		dir + " (also in $HARNESS_SHELL). Its README.md maps the layout and its rules."
}

// appendSection joins two prompt fragments with a blank line, tolerating an
// empty base (a session with no agent prompt).
func appendSection(base, section string) string {
	base = strings.TrimRight(base, "\n")
	if base == "" {
		return section
	}
	return base + "\n\n" + section
}

// statusSection says who moves the task's status and, when the task has an
// architect, how a session talks to it.
func statusSection(role domain.SessionRole) string {
	switch role {
	case domain.RoleDelegate:
		return `This task has an architect: the session that shaped it and delegated your part.
It owns the task status. Do not call update_task_status; it is refused.
Talk to the architect instead:

- mcp__task__message_architect — kind review_request when your plan or work is ready for review (attach the documents and artifacts by id, from .index.json); kind status_report at each milestone, when you finish, and as soon as you are blocked (status working, blocked, ready_for_review or done); kind question when you need a decision
- mcp__task__list_task_messages — your messages with the architect, to catch up on a reply you missed

Its replies arrive as a new turn starting with [task message … · reply …]. A reply
with verdict changes_requested means: make the changes, then ask for review again.
Sessions you start on this task also report to the architect.`
	case domain.RoleArchitect:
		return `You are this task's architect. You own its status: no other session started
under you can move it. Move it as the work moves, always with a reason, based on
what your delegates report:

- mcp__task__update_task_status — set this task's status with a reason: in_progress when the first delegate starts, review when the finished work is ready for the person, done when they accept it

Your delegates report to you. Their messages, the status checks that wake you
about them, and the person's answers to your review requests arrive as new turns
starting with [task message …], [status check …] or [review response …]: they
are your work queue. Answer each one:

- mcp__task__reply_to_session — answer a delegate; on a review_request, with verdict approved or changes_requested
- mcp__task__list_task_messages — every message between your delegates and you, to catch up
- mcp__task__request_user_review — ask the person to review a plan, a branch, a document or an artifact when the decision is theirs
- mcp__task__withdraw_user_review — withdraw a review request that is no longer needed
- mcp__task__list_review_requests — your review requests and where each stands
- mcp__task__set_status_check — change how often you are woken about a delegate (2–240 minutes), or 0 to pause
- mcp__task__list_status_checks — every status-check loop on this task
- mcp__task__merge_session_branch — once the person approved your review request about a delegate (about_session_id), have the server merge its branch into the default branch of the local checkout; it refuses, saying why, on a dirty checkout, a wrong branch or a conflict`
	}
	return `Move the task as the work moves, so the board stays true without a person
dragging the card:

- mcp__task__update_task_status — set this task's status: in_progress when you pick the work up, review when it is ready for a person to look at, done when you are told it is accepted`
}

// newSessionRole is the role a session about to start will have, worked out
// before it is stored: the architect if it starts in architect mode (it
// becomes the task's newest), a delegate if the session starting it is the
// architect or one of its delegates, a peer otherwise. Without a Roles lookup
// the server has no architect channel, and every session is a peer.
func (s *Service) newSessionRole(ctx context.Context, mode domain.SessionMode, parentID string) domain.SessionRole {
	if s.Roles == nil {
		return domain.RolePeer
	}
	if mode == domain.SessionModeArchitect {
		return domain.RoleArchitect
	}
	if parentID == "" {
		return domain.RolePeer
	}
	switch s.sessionRole(ctx, parentID) {
	case domain.RoleArchitect, domain.RoleDelegate:
		return domain.RoleDelegate
	}
	return domain.RolePeer
}

// sessionRole is what a stored session is on its task. A failed lookup briefs
// it as a peer rather than failing its start.
func (s *Service) sessionRole(ctx context.Context, id string) domain.SessionRole {
	if s.Roles == nil {
		return domain.RolePeer
	}
	role, err := s.Roles.SessionRole(ctx, id)
	if err != nil {
		slog.Warn("session role lookup failed; briefing it as a peer", "session", id, "err", err)
		return domain.RolePeer
	}
	return role
}

// maxAttachedInBrief caps the attached assets a brief lists; the rest are
// in task/designs/attached.md.
const maxAttachedInBrief = 20

// attachedAssetsSection tells a session which project assets are attached to
// its task: one line each, with the task that made it. Empty when none are.
func attachedAssetsSection(attached []*domain.Artifact) string {
	if len(attached) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Project assets attached to this task\n\n")
	b.WriteString("A person or another session attached these project design assets to this task.\n")
	b.WriteString("Build on them; task/designs/attached.md names their files under project/designs/.\n\n")
	for i, a := range attached {
		if i == maxAttachedInBrief {
			fmt.Fprintf(&b, "- +%d more: task/designs/attached.md\n", len(attached)-maxAttachedInBrief)
			break
		}
		fmt.Fprintf(&b, "- %s — %s (%s), from task %s\n", a.ID, a.Title, a.Kind, a.TicketID)
	}
	return strings.TrimRight(b.String(), "\n")
}

// attachedAssets is the project assets attached to tk, from another task.
// A failed lookup leaves them out: the brief is help, not a gate.
func (s *Service) attachedAssets(tk *domain.Ticket) []*domain.Artifact {
	if s.Artifacts == nil || tk == nil {
		return nil
	}
	list, err := s.Artifacts.ListArtifacts(context.Background(), ports.ArtifactFilter{TicketID: tk.ID})
	if err != nil {
		slog.Warn("list the task's attached assets for its brief", "ticket", tk.ID, "err", err)
		return nil
	}
	var attached []*domain.Artifact
	for _, a := range list {
		if a.TicketID != tk.ID {
			attached = append(attached, a)
		}
	}
	return attached
}

// projectBriefInput is what a project-level session (a product session with
// no task, ADR 0016) is briefed on: the project, its repositories and its
// bounded contexts. Repositories and Contexts may be empty.
type projectBriefInput struct {
	Project      *domain.Project
	Repositories []*domain.Repository
	Contexts     []*domain.BoundedContext
	// AgentsMove is the setting "Agents move documents to project" as the
	// session starts.
	AgentsMove bool
}

// briefProject gathers the brief of a project-level session on p. A failed
// listing leaves that part out: the brief is help, not a gate.
func (s *Service) briefProject(p *domain.Project) projectBriefInput {
	in := projectBriefInput{Project: p}
	if p == nil {
		return in
	}
	if s.catalog.Repositories != nil {
		repos, err := s.catalog.Repositories.ListRepositories(context.Background(), p.ID)
		if err != nil {
			slog.Warn("list the project's repositories for its brief", "project", p.ID, "err", err)
		}
		in.Repositories = repos
	}
	if s.catalog.Contexts != nil {
		in.Contexts = s.catalog.Contexts.ListBoundedContexts(p.ID)
	}
	return in
}

// applyProjectContext is applyTaskContext for a session that has no task:
// the per-session task MCP server and its allow-list as for a task session
// (the tools that need a task answer SESSION_HAS_NO_TASK), and the project
// brief in place of the task brief.
func applyProjectContext(cfg *llmkit.SessionConfig, in projectBriefInput, taskServerURL string) {
	if in.Project == nil {
		return
	}
	if taskServerURL != "" {
		cfg.MCPServers = append(cfg.MCPServers, llmkit.MCPServerSpec{
			Name:      taskServerName,
			Transport: "http",
			URL:       taskServerURL,
		})
	}
	for _, name := range tooling.SessionTaskToolNames {
		cfg.AllowedTools = append(cfg.AllowedTools, llmkit.MCPToolRef(taskServerName, name))
	}
	cfg.AppendSystem = appendSection(cfg.AppendSystem, projectBrief(in))
}

// projectBrief is the block describing the project a session with no task
// works on, the virtual shell holding its documents, how to create the
// feature task and its children, and which task tools do not apply here.
func projectBrief(in projectBriefInput) string {
	var b strings.Builder
	p := in.Project
	b.WriteString("## Your project\n\n")
	b.WriteString("You are working on project " + p.Name + " with no task: a product session that defines a feature.\n")
	b.WriteString("You create the feature task yourself, then its PRD, then one child task and one brief per piece of work.\n")
	if len(in.Repositories) > 0 {
		b.WriteString("\nRepositories (one application each):\n\n")
		for _, r := range in.Repositories {
			line := "- " + r.Name
			if d := strings.TrimSpace(r.Description); d != "" {
				line += " — " + d
			}
			b.WriteString(line + "\n")
		}
	}
	if len(in.Contexts) > 0 {
		b.WriteString("\nBounded contexts:\n\n")
		for _, c := range in.Contexts {
			line := "- " + c.Name
			if d := strings.TrimSpace(c.Purpose); d != "" {
				line += " — " + d
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString(`
The project's documents, designs and every task's documents are files in
your virtual shell, $HARNESS_SHELL ("Your virtual shell" below names its
path; its README.md maps the layout and the rules). Work on them with your
ordinary tools; none of it takes a tool call:

- Read with Read, grep, ls and sed -n. .index.json lists every file with its id, kind, scope and version.
- Write with Write, Edit, sed -i, tee, cp, mv, rm, touch and mkdir. Each change is checked before it lands and stored once it has. Shell redirects (>, >>) into the folder are refused: they would not be stored.

What the folders hold:

- tasks/<id>/docs/ — a task's documents, one folder per task of the project (ls tasks/ lists their ids). A file you create there is a new document on that task, titled by its first heading. This is where the PRD goes (under the feature task) and each brief (under its child). Any text format: .md, .html, .txt.
- project/docs/ — the project's documents: architecture, conventions, decisions, the specs and contracts tasks build on. Read them before the interview. ` + projectMoveLine(in.AgentsMove) + `
- project/designs/ — the project's design assets. Read-only here.

This session has no task, so there is no task/ folder: no task.json, comments,
resources or designs of its own.

Files can be large. grep for what you need, read that range with sed -n or
Read with offset and limit, never the whole file; change a part with Edit
or sed -i rather than rewriting it.

A refused write says why, starting with its code:

- BASE_VERSION_STALE — the file changed since you got it and has been refreshed: read it again and redo the change
- READ_ONLY — the path is not yours to change
- REDIRECT_DENIED — write with Write, Edit or tee instead of > or >>
- TASK_NOT_FOUND, TASK_NOT_ON_PROJECT — tasks/<id> names no task of this project: create it first with mcp__task__create_task
- NOT_STORED — the path is outside the folders the harness stores (here: tasks/<id>/docs and project/docs); nothing was written
- SHELL_UNAVAILABLE — the harness could not be reached: redo the change once it is back

The project's tasks, and how to create the feature and its children:

- mcp__task__list_project_tasks — the project's tasks: id, title, status, parent_id; with parent_id, the children of one
- mcp__task__create_task — create a task in this project. Pass parent_id "" explicitly for the feature task (there is no session task to default to), then the feature task's id for each child

To see the project around the feature, and who will implement it:

- mcp__task__list_project_repositories — the project's repositories: its applications
- mcp__task__list_bounded_contexts — its bounded contexts, with their language and zones
- mcp__task__list_agents — the agents a session can run as, and what each is for

To ask for review once the PRD and the briefs are written, name the feature
task with task_id: only a session with no task of its own passes it.

- mcp__task__request_user_review — task_id the feature task: attach the PRD (its id in .index.json; only the feature task's own documents attach) and name each child task by id and title in the body. The person's answer comes back to you as a turn
- mcp__task__update_task_status — task_id the feature task: move it to review with a reason when you ask for the review

The tools that act on a session's own task answer SESSION_HAS_NO_TASK here:
mcp__task__get_task, mcp__task__start_task_session, mcp__task__list_task_sessions,
the rest of the architect channel, mcp__task__publish_artifact and the attach
tools; mcp__task__request_user_review and mcp__task__update_task_status do too
without task_id. If the review cannot be opened, tell the person with notify.

These tools always act on this session's project; they take no project id.`)
	return b.String() + "\n\n" + notificationsSection
}
