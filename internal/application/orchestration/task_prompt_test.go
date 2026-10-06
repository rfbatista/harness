package orchestration

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
)

func TestApplyTaskContext(t *testing.T) {
	cfg := llmkit.SessionConfig{AppendSystem: "be terse", AllowedTools: []string{"Read"}}
	applyTaskContext(&cfg, &domain.Ticket{
		ID: "tk1", Title: "Ship the thing", Description: "with care", Status: domain.TicketStatusInProgress,
	}, "http://127.0.0.1:8080/mcp/task/s1")

	if len(cfg.MCPServers) != 1 {
		t.Fatalf("want exactly one MCP server, got %+v", cfg.MCPServers)
	}
	// The server name must be "task": the brief names its tools mcp__task__*.
	got := cfg.MCPServers[0]
	if got.Name != "task" || got.Transport != "http" || got.URL != "http://127.0.0.1:8080/mcp/task/s1" {
		t.Fatalf("task server not attached: %+v", got)
	}
	// The agent's own prompt survives, with the brief after it.
	if !strings.HasPrefix(cfg.AppendSystem, "be terse\n\n") {
		t.Fatalf("agent prompt lost:\n%s", cfg.AppendSystem)
	}
	for _, want := range []string{"tk1", "Ship the thing", "with care", "in_progress"} {
		if !strings.Contains(cfg.AppendSystem, want) {
			t.Fatalf("brief missing %q:\n%s", want, cfg.AppendSystem)
		}
	}
	// Reading and writing its own task must not stop for an approval.
	if !slices.Contains(cfg.AllowedTools, "Read") {
		t.Fatal("caller-supplied allowed tools dropped")
	}
	for _, name := range tooling.SessionTaskToolNames {
		ref := "mcp__task__" + name
		if !slices.Contains(cfg.AllowedTools, ref) {
			t.Fatalf("%s not allow-listed: %v", ref, cfg.AllowedTools)
		}
	}
}

func TestApplyTaskContext_NoTask(t *testing.T) {
	cfg := llmkit.SessionConfig{AppendSystem: "be terse"}
	applyTaskContext(&cfg, nil, "http://127.0.0.1:8080/mcp/task/s1")
	if len(cfg.MCPServers) != 0 || cfg.AppendSystem != "be terse" || len(cfg.AllowedTools) != 0 {
		t.Fatalf("a session with no task must be untouched: %+v", cfg)
	}
}

// A host that serves no per-session task endpoint still gets the brief and the
// allow-list; only the server entry is skipped, so the CLI is never handed an
// MCP server with an empty URL.
func TestApplyTaskContext_NoTaskServerURL(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "")
	if len(cfg.MCPServers) != 0 {
		t.Fatalf("no url must mean no server entry: %+v", cfg.MCPServers)
	}
	if !strings.Contains(cfg.AppendSystem, "tk1") {
		t.Fatalf("brief missing:\n%s", cfg.AppendSystem)
	}
	if !slices.Contains(cfg.AllowedTools, "mcp__task__"+tooling.SessionTaskToolNames[0]) {
		t.Fatalf("allow-list missing: %v", cfg.AllowedTools)
	}
}

// With no agent prompt the brief stands alone, without a leading blank line.
func TestApplyTaskContext_NoAgentPrompt(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "http://127.0.0.1:8080/mcp/task/s1")
	if !strings.HasPrefix(cfg.AppendSystem, "## Your task") {
		t.Fatalf("unexpected prompt start:\n%s", cfg.AppendSystem)
	}
}

// The brief is how an agent learns the tool exists and when to move the task,
// so the board does not wait for a person to drag the card.
func TestApplyTaskContext_BriefNamesUpdateTaskStatus(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "")
	for _, want := range []string{"mcp__task__update_task_status", "in_progress", "review", "done"} {
		if !strings.Contains(cfg.AppendSystem, want) {
			t.Fatalf("brief missing %q:\n%s", want, cfg.AppendSystem)
		}
	}
	// One line per tool, in the style of the others.
	if !strings.Contains(cfg.AppendSystem, "\n- mcp__task__update_task_status — ") {
		t.Fatalf("tool line not in the list style:\n%s", cfg.AppendSystem)
	}
}
