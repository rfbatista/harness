package orchestration

import (
	"strings"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
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
func applyTaskContext(cfg *llmkit.SessionConfig, tk *domain.Ticket, taskServerURL string) {
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
	cfg.AppendSystem = appendSection(cfg.AppendSystem, taskBrief(tk))
}

// taskBrief is the block describing the task and the tools that reach it.
func taskBrief(tk *domain.Ticket) string {
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

Move the task as the work moves, so the board stays true without a person
dragging the card:

- mcp__task__update_task_status — set this task's status: in_progress when you pick the work up, review when it is ready for a person to look at, done when you are told it is accepted

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
- mcp__task__list_task_artifacts — what every session on this task has published
- mcp__task__unpublish_artifact — take one of yours down (the file stays)

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
