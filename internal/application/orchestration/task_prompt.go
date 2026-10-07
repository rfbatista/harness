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
// into, the documents linked to it, and the other sessions working on it. The literal matters: taskBrief names its
// tools as mcp__task__* in the system prompt, and the two have to agree.
const taskServerName = "task"

// applyTaskContext gives a session spawned into a task everything it needs to
// work on it: the per-session task MCP server, an allow-list so reading and
// writing its own task never stops for an approval, and a short brief appended
// to the system prompt so the agent knows about the task without a tool call.
// Document bodies stay behind the tools, so the prompt stays small.
//
// taskServerURL is empty when the host serves no per-session task endpoint; the
// brief and the allow-list still apply, only the server entry is skipped.
//
// role is what the session is on its task: a delegate is told to report to the
// architect instead of moving the status, the architect that it owns the
// status and how its delegates reach it; a peer gets the brief unchanged.
func applyTaskContext(cfg *llmkit.SessionConfig, tk *domain.Ticket, taskServerURL string, role domain.SessionRole) {
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
	cfg.AppendSystem = appendSection(cfg.AppendSystem, taskBrief(tk, role))
}

// taskBrief is the block describing the task and the tools that reach it.
// Only the paragraph about the task's status depends on role.
func taskBrief(tk *domain.Ticket, role domain.SessionRole) string {
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
Documents are how a task carries context between sessions. Read what earlier
sessions left, and write down what the next one will need:

- mcp__task__get_task — this task and the documents linked to it
- mcp__task__list_task_documents — the linked documents, titles only
- mcp__task__read_task_document — one document, with its content and format
- mcp__task__create_task_document — a new document, linked to this task for you
- mcp__task__update_task_document — revise one of them

A document you write is a complete HTML document, not Markdown.
Markdown is refused with DOCUMENT_NOT_HTML and nothing is stored; rewrite it
as a page. The minimal shape:

<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Plan: …</title><style>/* inline styles */</style></head>
<body>…</body>
</html>

Keep styles inline or in <style>; nothing outside the harness origin loads.
To match the harness look, link <link rel="stylesheet" href="/static/document.css">
in <head> and put class="prose" on <body>; it follows the system's light or
dark scheme. Make it readable in light and dark (color-scheme: light dark, or a
prefers-color-scheme media query). It renders on the task's documents page
in a sandboxed frame: scripts run there but cannot reach the harness API.
Older documents may be Markdown; read_task_document says which.

Some knowledge outlives one task: architecture, conventions, decisions, and
the specs and contracts other tasks will build on. Those are project
documents: every session of the project, on any task, can read and revise
them, and they show on the project's documents page. Move a document there
once it says something the next task needs, not just this one; its link to
this task stays. Only documents linked to this task can be moved:

- mcp__task__list_project_documents — the project's documents, titles only
- mcp__task__read_project_document — one project document, with its content and format
- mcp__task__update_project_document — revise a project document; new content must be a complete HTML page
- mcp__task__move_document_to_project — make one of this task's documents a project document
- mcp__task__move_document_to_task — move one of them back to this task's scope

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

To show what you make to the person in the web UI's Design tab, live, while you keep talking here:

- mcp__task__publish_artifact — publish a file from your worktree (an HTML page or component, an image, a video, any file) or a dev server's loopback URL; publish again to refresh it
- mcp__task__list_task_artifacts — what every session on this task has published, with each one's scope, and the project assets attached to this task
- mcp__task__unpublish_artifact — take one of yours down (the file stays); a project asset is moved back to the task first

Some design assets outlive one task: a logo, a palette, a component, a
reference screen other tasks will reuse. Those are project assets: the
harness keeps its own copy, so they outlive this session and its worktree,
and they show in the project's design-assets library. Before you make a new
asset, check list_project_artifacts for one to build on. Move an asset there
once other tasks will reuse it; a dev-server URL cannot move. Only artifacts
published on this task can be moved:

- mcp__task__list_project_artifacts — the project's design assets, from any task: title, kind, note, revision, task, view URL
- mcp__task__move_artifact_to_project — make one of this task's artifacts a project asset
- mcp__task__move_artifact_to_task — move one of them back to this task's scope

A project asset can also be attached to this task, by a person or by you, so
it shows in this task's Design tab beside what the task produced:

- mcp__task__attach_artifact_to_task — attach a project asset, from any task of this project, to this task
- mcp__task__detach_artifact_from_task — detach one from this task; it stays in the project

These tools always act on this task; they take no project or task id.`)
	return b.String()
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

- mcp__task__message_architect — kind review_request when your plan or work is ready for review (attach the documents and artifacts by id); kind status_report at each milestone, when you finish, and as soon as you are blocked (status working, blocked, ready_for_review or done); kind question when you need a decision
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
- mcp__task__list_status_checks — every status-check loop on this task`
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
// one list_task_artifacts away.
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
	b.WriteString("Build on them; list_task_artifacts has their view URLs.\n\n")
	for i, a := range attached {
		if i == maxAttachedInBrief {
			fmt.Fprintf(&b, "- +%d more: list_task_artifacts\n", len(attached)-maxAttachedInBrief)
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
