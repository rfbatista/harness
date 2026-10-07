package orchestration

import (
	"os"
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
	}, "http://127.0.0.1:8080/mcp/task/s1", domain.RolePeer)

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
	applyTaskContext(&cfg, nil, "http://127.0.0.1:8080/mcp/task/s1", domain.RolePeer)
	if len(cfg.MCPServers) != 0 || cfg.AppendSystem != "be terse" || len(cfg.AllowedTools) != 0 {
		t.Fatalf("a session with no task must be untouched: %+v", cfg)
	}
}

// A host that serves no per-session task endpoint still gets the brief and the
// allow-list; only the server entry is skipped, so the CLI is never handed an
// MCP server with an empty URL.
func TestApplyTaskContext_NoTaskServerURL(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer)
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
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "http://127.0.0.1:8080/mcp/task/s1", domain.RolePeer)
	if !strings.HasPrefix(cfg.AppendSystem, "## Your task") {
		t.Fatalf("unexpected prompt start:\n%s", cfg.AppendSystem)
	}
}

// The brief is how an agent learns the tool exists and when to move the task,
// so the board does not wait for a person to drag the card.
func TestApplyTaskContext_BriefNamesUpdateTaskStatus(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer)
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

// Documents are HTML pages. The brief is where an agent learns the rule
// before its first create_task_document, so it does not learn it from a refusal.
func TestApplyTaskContext_BriefSaysDocumentsAreHTML(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer)
	for _, want := range []string{
		"complete HTML document",
		"<!doctype html>",
		"<meta charset=\"utf-8\">",
		"<title>",
		"<body>",
		"Markdown is refused",
		"DOCUMENT_NOT_HTML",
		"light and dark",
		"/static/document.css",
	} {
		if !strings.Contains(cfg.AppendSystem, want) {
			t.Errorf("brief missing %q:\n%s", want, cfg.AppendSystem)
		}
	}
	if strings.Contains(cfg.AppendSystem, "markdown document") {
		t.Errorf("brief still describes markdown documents:\n%s", cfg.AppendSystem)
	}
}

// The brief is where an agent learns that some documents outlive the task,
// and which tools move and reach them.
func TestApplyTaskContext_BriefNamesProjectDocuments(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer)
	for _, want := range []string{
		"\n- mcp__task__list_project_documents — ",
		"\n- mcp__task__read_project_document — ",
		"\n- mcp__task__update_project_document — ",
		"\n- mcp__task__move_document_to_project — ",
		"\n- mcp__task__move_document_to_task — ",
		"project document", "architecture", "conventions", "decisions", "outlive",
	} {
		if !strings.Contains(cfg.AppendSystem, want) {
			t.Errorf("brief missing %q:\n%s", want, cfg.AppendSystem)
		}
	}
}

// Every tool on the task server is named in the brief, in the list style,
// so the two never drift apart.
// The architect channel's tools are named only in the briefs of the roles that
// use them (a delegate's and the architect's); a peer's brief stays as it was.
var channelTools = []string{
	"message_architect", "reply_to_session", "list_task_messages", "request_user_review",
	"withdraw_user_review", "list_review_requests", "set_status_check", "list_status_checks",
}

func TestApplyTaskContext_BriefNamesEveryTaskTool(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer)
	for _, name := range tooling.SessionTaskToolNames {
		if slices.Contains(channelTools, name) {
			continue
		}
		if !strings.Contains(cfg.AppendSystem, "\n- mcp__task__"+name+" — ") {
			t.Errorf("brief does not list mcp__task__%s", name)
		}
	}
}

// The brief is where an agent learns that some design assets outlive the
// task, to look for them before making new ones, and which tools move them.
func TestApplyTaskContext_BriefNamesProjectArtifacts(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer)
	for _, want := range []string{
		"\n- mcp__task__list_project_artifacts — ",
		"\n- mcp__task__move_artifact_to_project — ",
		"\n- mcp__task__move_artifact_to_task — ",
		"project asset", "logo", "palette", "outlive", "Before you make a new\nasset, check list_project_artifacts",
	} {
		if !strings.Contains(cfg.AppendSystem, want) {
			t.Errorf("brief missing %q:\n%s", want, cfg.AppendSystem)
		}
	}
}

// A session with no architect keeps today's brief, byte for byte.
func TestTaskBrief_PeerIsUnchanged(t *testing.T) {
	want, err := os.ReadFile("testdata/peer_brief.golden")
	if err != nil {
		t.Fatal(err)
	}
	tk := &domain.Ticket{ID: "tk1", Title: "Ship the thing", Description: "with care", Status: domain.TicketStatusInProgress}
	if got := taskBrief(tk, domain.RolePeer); got != string(want) {
		t.Fatalf("the peer brief changed:\n%s", got)
	}
}

// A delegate reports to the architect instead of moving the status, and is
// told how the architect's replies reach it.
func TestTaskBrief_DelegateTalksToTheArchitect(t *testing.T) {
	got := taskBrief(&domain.Ticket{ID: "tk1", Title: "Ship the thing"}, domain.RoleDelegate)
	for _, want := range []string{
		"\n- mcp__task__message_architect — ", "\n- mcp__task__list_task_messages — ",
		"review_request", "status_report", "blocked", "Do not call update_task_status",
		"[task message", "changes_requested",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("delegate brief lacks %q", want)
		}
	}
	if strings.Contains(got, "\n- mcp__task__update_task_status — ") {
		t.Error("the delegate brief still offers update_task_status")
	}
	assertNamesTheOtherTools(t, got)
}

// The architect owns the status and works the turns its delegates send it.
func TestTaskBrief_ArchitectOwnsTheStatus(t *testing.T) {
	got := taskBrief(&domain.Ticket{ID: "tk1", Title: "Ship the thing"}, domain.RoleArchitect)
	for _, name := range channelTools {
		if name == "message_architect" {
			continue // the architect does not message itself
		}
		if !strings.Contains(got, "\n- mcp__task__"+name+" — ") {
			t.Errorf("architect brief does not list mcp__task__%s", name)
		}
	}
	for _, want := range []string{"\n- mcp__task__update_task_status — ", "reason", "[task message", "[status check", "[review response"} {
		if !strings.Contains(got, want) {
			t.Errorf("architect brief lacks %q", want)
		}
	}
	assertNamesTheOtherTools(t, got)
}

// Whatever its role, a session still learns every tool outside the channel.
func assertNamesTheOtherTools(t *testing.T, brief string) {
	t.Helper()
	for _, name := range tooling.SessionTaskToolNames {
		if slices.Contains(channelTools, name) || name == "update_task_status" {
			continue
		}
		if !strings.Contains(brief, "\n- mcp__task__"+name+" — ") {
			t.Errorf("brief does not list mcp__task__%s", name)
		}
	}
}
