package orchestration

import (
	"strings"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
)

// taskServerName is the MCP server exposing the task the session was spawned
// into and the documents linked to it. The literal matters: taskBrief names its
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
- mcp__task__read_task_document — one document, with its content
- mcp__task__create_task_document — a new document, linked to this task for you
- mcp__task__update_task_document — revise one of them

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
