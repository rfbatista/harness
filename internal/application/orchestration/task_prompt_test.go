package orchestration

import (
	"os"
	"regexp"
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
	}, "http://127.0.0.1:8080/mcp/task/s1", domain.RolePeer, taskCounts{})

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
	applyTaskContext(&cfg, nil, "http://127.0.0.1:8080/mcp/task/s1", domain.RolePeer, taskCounts{})
	if len(cfg.MCPServers) != 0 || cfg.AppendSystem != "be terse" || len(cfg.AllowedTools) != 0 {
		t.Fatalf("a session with no task must be untouched: %+v", cfg)
	}
}

// A host that serves no per-session task endpoint still gets the brief and the
// allow-list; only the server entry is skipped, so the CLI is never handed an
// MCP server with an empty URL.
func TestApplyTaskContext_NoTaskServerURL(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer, taskCounts{})
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
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "http://127.0.0.1:8080/mcp/task/s1", domain.RolePeer, taskCounts{})
	if !strings.HasPrefix(cfg.AppendSystem, "## Your task") {
		t.Fatalf("unexpected prompt start:\n%s", cfg.AppendSystem)
	}
}

// The brief is how an agent learns the tool exists and when to move the task,
// so the board does not wait for a person to drag the card.
func TestApplyTaskContext_BriefNamesUpdateTaskStatus(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer, taskCounts{})
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

// The brief is where an agent learns its resources are files in its
// virtual shell: where each kind lives, how to read and write them, and the
// refusal codes it acts on.
func TestApplyTaskContext_BriefTeachesTheShell(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer, taskCounts{})
	for _, want := range []string{
		"$HARNESS_SHELL", "README.md", ".index.json",
		"\n- task/docs/ — ", "\n- project/docs/ — ", "\n- task/comments/ — ", "\n- task/resources/ — ",
		"\n- task/designs/ — ", "\n- project/designs/ — ",
		"Read, grep", "sed -n", "Write, Edit, sed -i, tee, cp, mv, rm",
		"architecture, conventions, decisions", "mv a document from task/docs/ to project/docs/",
		"\n- BASE_VERSION_STALE — ", "\n- READ_ONLY, COMMENT_NOT_YOURS — ", "\n- REDIRECT_DENIED — ",
		"\n- NOT_ON_TASK — ", "\n- NOT_STORED — ", "\n- SHELL_UNAVAILABLE — ",
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
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer, taskCounts{})
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
// task, to look for them before making new ones, and how to move them.
func TestApplyTaskContext_BriefNamesProjectAssets(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyTaskContext(&cfg, &domain.Ticket{ID: "tk1", Title: "Ship the thing"}, "", domain.RolePeer, taskCounts{})
	for _, want := range []string{
		"logo", "palette", "outlives", "Look here before you make a new asset",
		"mv a design from task/designs/ to project/designs/", "task/designs/attached.md",
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
	if got := taskBrief(tk, domain.RolePeer, taskCounts{}); got != string(want) {
		t.Fatalf("the peer brief changed:\n%s", got)
	}
}

// A delegate reports to the architect instead of moving the status, and is
// told how the architect's replies reach it.
func TestTaskBrief_DelegateTalksToTheArchitect(t *testing.T) {
	got := taskBrief(&domain.Ticket{ID: "tk1", Title: "Ship the thing"}, domain.RoleDelegate, taskCounts{})
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
	got := taskBrief(&domain.Ticket{ID: "tk1", Title: "Ship the thing"}, domain.RoleArchitect, taskCounts{})
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

// Every role's brief names the inbox tools and keeps notify for what the
// person has to know or do.
func TestTaskBrief_NamesTheNotificationTools(t *testing.T) {
	for _, role := range []domain.SessionRole{domain.RolePeer, domain.RoleDelegate, domain.RoleArchitect} {
		got := taskBrief(&domain.Ticket{ID: "tk1", Title: "Ship the thing"}, role, taskCounts{})
		for _, want := range []string{"\n- mcp__task__list_notifications — ", "\n- mcp__task__notify — ", "never for progress", "already notify the"} {
			if !strings.Contains(got, want) {
				t.Errorf("%q brief lacks %q", role, want)
			}
		}
	}
}

// removedTaskTools are the tools the virtual shell replaced (contract
// "Harness server ↔ Agent sessions — virtual shell" §7).
var removedTaskTools = []string{
	"list_task_documents", "read_task_document", "create_task_document", "update_task_document", "edit_task_document",
	"list_project_documents", "read_project_document", "update_project_document", "edit_project_document",
	"move_document_to_project", "move_document_to_task", "search_documents",
	"add_task_comment", "update_task_comment", "delete_task_comment", "list_task_comments",
	"list_task_resources", "read_task_resource",
	"list_task_artifacts", "list_project_artifacts", "read_artifact", "unpublish_artifact",
	"move_artifact_to_project", "move_artifact_to_task",
}

// No removed tool is named anywhere a session is told what to do: the
// brief for each role and its sections, each mode's opening message, and
// every file of the mode skills. Nor does any of them still require HTML
// documents (ADR 0004).
func TestPrompts_NameNoRemovedTool(t *testing.T) {
	tk := &domain.Ticket{ID: "tk1", Title: "Ship the thing"}
	texts := map[string]string{
		"shell":     shellSection("/tmp/harness-shell-1"),
		"resources": resourcesSection(taskCounts{1, 2, true}),
		"attached":  attachedAssetsSection([]*domain.Artifact{{ID: "a1", TicketID: "tk9", Title: "Logo", Kind: domain.ArtifactPage}}),
	}
	for _, role := range []domain.SessionRole{domain.RolePeer, domain.RoleDelegate, domain.RoleArchitect} {
		texts["brief/"+string(role)] = taskBrief(tk, role, taskCounts{})
	}
	for mode, names := range modeSkillNames {
		texts["prompt/"+string(mode)] = modePrompt(mode, tk, "")
		for _, skill := range names {
			files, err := modeSkillFiles(skill)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				texts[skill+"/"+f.Path] = f.Content
			}
		}
	}
	for where, text := range texts {
		for _, name := range removedTaskTools {
			if regexp.MustCompile(`\b` + name + `\b`).MatchString(text) {
				t.Errorf("%s names the removed tool %s", where, name)
			}
		}
		for _, rule := range []string{"DOCUMENT_NOT_HTML", "Markdown is refused", "complete HTML document", "complete HTML page"} {
			if strings.Contains(text, rule) {
				t.Errorf("%s still requires HTML documents (%q)", where, rule)
			}
		}
	}
}

// Every task tool a prompt names exists: the brief for each role and the
// mode skills never point an agent at a tool the server does not serve.
func TestPrompts_NameOnlyRealTaskTools(t *testing.T) {
	texts := map[string]string{}
	for _, role := range []domain.SessionRole{domain.RolePeer, domain.RoleDelegate, domain.RoleArchitect} {
		texts["brief/"+string(role)] = taskBrief(&domain.Ticket{ID: "tk1", Title: "Ship the thing"}, role, taskCounts{})
	}
	for _, names := range modeSkillNames {
		for _, skill := range names {
			files, err := modeSkillFiles(skill)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				texts[skill+"/"+f.Path] = f.Content
			}
		}
	}
	named := regexp.MustCompile(`mcp__task__([a-z_]+)`)
	for where, text := range texts {
		for _, m := range named.FindAllStringSubmatch(text, -1) {
			if !slices.Contains(tooling.SessionTaskToolNames, m[1]) {
				t.Errorf("%s names mcp__task__%s, which is not a task tool", where, m[1])
			}
		}
	}
}

// The brief tells every session to read large files in parts and to edit
// them in parts.
func TestTaskBrief_ReadsAndEditsFilesInParts(t *testing.T) {
	got := taskBrief(&domain.Ticket{ID: "tk1", Title: "Ship the thing"}, domain.RolePeer, taskCounts{})
	for _, want := range []string{"grep for what you need", "sed -n", "offset and limit, never the whole file", "change a part with Edit\nor sed -i"} {
		if !strings.Contains(got, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
}
