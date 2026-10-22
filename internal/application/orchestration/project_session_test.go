package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A product-mode session may start on a project with no task (ADR 0016): it
// is recorded with no ticket, named after the project, on a branch that no
// task title names, with the task server attached and a project brief in
// place of the task brief.
func TestStartInteractive_ProductWithoutATask(t *testing.T) {
	svc, prov := newInteractiveService(t)
	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: "product", Prompt: "Export to CSV."})

	if sess.TicketID != "" || sess.Mode != domain.SessionModeProduct || sess.Task != "Product: harness" || !sess.Interactive {
		t.Fatalf("session = %+v", sess)
	}
	if got := sessionOf(svc, sess.ID); got == nil || got.TicketID != "" || got.Mode != domain.SessionModeProduct {
		t.Fatalf("stored session = %+v", got)
	}
	if !strings.HasPrefix(sess.Branch, "agent/product-harness-") || strings.Contains(sess.Branch, "ship-the-thing") {
		t.Fatalf("branch = %q, want one named after the project, not a task", sess.Branch)
	}
	if len(prov.created) != 1 || prov.created[0].Branch != sess.Branch {
		t.Fatalf("worktree not provisioned on the session's branch: %+v", prov.created)
	}

	var task bool
	for _, sv := range launch.Spec.MCPServers {
		task = task || sv.Name == "task"
	}
	if !task {
		t.Fatalf("task server not attached: %+v", launch.Spec.MCPServers)
	}
	p := launch.Spec.Prompt
	if !strings.HasPrefix(p, "Use the task-product skill on project harness") || !strings.HasSuffix(p, "Export to CSV.") {
		t.Errorf("first message = %q", p)
	}
	for _, want := range []string{"create_task", `parent_id ""`, "tasks/<id>/docs/", "SESSION_HAS_NO_TASK"} {
		if !strings.Contains(p, want) {
			t.Errorf("first message lacks %q:\n%s", want, p)
		}
	}
	sys := launch.Spec.AppendSystem
	if !strings.Contains(sys, "## Your project") || strings.Contains(sys, "## Your task") {
		t.Errorf("system prompt has no project brief, or a task brief:\n%s", sys)
	}
	for _, want := range []string{"harness", "\n- api", "mcp__task__create_task"} {
		if !strings.Contains(sys, want) {
			t.Errorf("project brief lacks %q:\n%s", want, sys)
		}
	}
}

// Every other mode still needs a task; the check comes after the mode is
// parsed, so an unknown mode without a ticket is reported as such.
func TestStartInteractive_OnlyProductStartsWithoutATask(t *testing.T) {
	for _, mode := range []string{"", "architect", "implement", "design"} {
		t.Run("mode "+mode, func(t *testing.T) {
			svc, prov := newInteractiveService(t)
			_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: mode})
			if codeOf(err) != "INVALID_INPUT" || !strings.Contains(err.Error(), "ticket_id is required") {
				t.Fatalf("err = %v, want INVALID_INPUT ticket_id is required", err)
			}
			if len(prov.created) != 0 {
				t.Fatalf("a rejected request provisioned a worktree: %v", prov.created)
			}
		})
	}
	svc, _ := newInteractiveService(t)
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: "wizard"})
	if codeOf(err) != "INVALID_INPUT" || strings.Contains(err.Error(), "ticket_id") {
		t.Fatalf("unknown mode without a ticket = %v, want the mode's own INVALID_INPUT", err)
	}
	// The project and a repository stay required.
	for name, req := range map[string]InteractiveRequest{
		"no project":    {RepositoryID: "r1", Mode: "product"},
		"no repository": {ProjectID: "p1", Mode: "product"},
	} {
		if _, _, err := svc.StartInteractive(context.Background(), req); codeOf(err) != "INVALID_INPUT" {
			t.Errorf("%s = %v, want INVALID_INPUT", name, err)
		}
	}
}

// A ticket-less session resumes like any other: same worktree, the product
// skill and the project brief again, still with no ticket.
func TestResumeInteractive_TicketLessProductSession(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: "product"})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	resumed, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.TicketID != "" || resumed.Status != domain.SessionRunning || spec.Dir != sess.WorkingDir {
		t.Fatalf("resumed = %+v, spec dir %q", resumed, spec.Dir)
	}
	pluginFile(t, launchOf(t, spec), "task-product", "PRD-FORMAT.md")
	if !strings.Contains(spec.AppendSystem, "## Your project") || strings.Contains(spec.AppendSystem, "## Your task") {
		t.Fatalf("resumed system prompt:\n%s", spec.AppendSystem)
	}
}

// With no task, only a product-mode session gets an opening, on its
// project: the interview, then the feature task first, then the PRD and the
// briefs under tasks/<id>/docs/. Any other mode's first message is what the
// person typed.
func TestModePrompt_ProductWithoutATask(t *testing.T) {
	project := &domain.Project{ID: "p1", Name: "harness"}
	p := modePrompt(domain.SessionModeProduct, nil, project, "Keep it to CSV.")
	if !strings.HasPrefix(p, "Use the task-product skill on project harness") || !strings.HasSuffix(p, "Keep it to CSV.") {
		t.Errorf("first message = %q", p)
	}
	for _, want := range []string{"goal", "users", "scope", "risks", "create_task", `parent_id ""`, "PRD: ", "tasks/<id>/docs/",
		"PRD-FORMAT.md", "TASK-BRIEF-FORMAT.md", "SESSION_HAS_NO_TASK", "notify", "Never write code", "never start implementation sessions"} {
		if !strings.Contains(p, want) {
			t.Errorf("product prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "task/docs/") || strings.Contains(p, "defaults to this task") {
		t.Errorf("product prompt still talks about a session task:\n%s", p)
	}
	if got := modePrompt(domain.SessionModeProduct, nil, project, ""); strings.HasSuffix(got, "\n") || strings.Contains(got, "\n\n\n") {
		t.Errorf("an empty typed prompt leaves a trailing gap: %q", got)
	}
	for _, mode := range []domain.SessionMode{domain.SessionModeArchitect, domain.SessionModeImplement, domain.SessionModeDesign, ""} {
		if got := modePrompt(mode, nil, project, "hi"); got != "hi" {
			t.Errorf("mode %q without a task opened with %q, want what was typed", mode, got)
		}
	}
	if got := modePrompt(domain.SessionModeProduct, nil, nil, "hi"); got != "hi" {
		t.Errorf("product without a project opened with %q, want what was typed", got)
	}
}

// The project brief replaces the task brief for a session with no task: the
// project, its repositories and bounded contexts, the shell without task/,
// how to create the feature task and its children, and which task tools
// answer SESSION_HAS_NO_TASK. The task server and its allow-list stay.
func TestApplyProjectContext_BriefsTheProject(t *testing.T) {
	cfg := llmkit.SessionConfig{AppendSystem: "be terse", AllowedTools: []string{"Read"}}
	applyProjectContext(&cfg, projectBriefInput{
		Project:      &domain.Project{ID: "p1", Name: "harness"},
		Repositories: []*domain.Repository{{ID: "r1", Name: "api", Description: "the server"}, {ID: "r2", Name: "web"}},
		Contexts:     []*domain.BoundedContext{{ID: "bc1", Name: "Billing", Purpose: "invoices and payments"}},
	}, "http://127.0.0.1:8080/mcp/task/s1")

	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "task" || cfg.MCPServers[0].URL != "http://127.0.0.1:8080/mcp/task/s1" {
		t.Fatalf("task server not attached: %+v", cfg.MCPServers)
	}
	if !strings.HasPrefix(cfg.AppendSystem, "be terse\n\n## Your project\n\n") {
		t.Fatalf("agent prompt lost or brief not after it:\n%s", cfg.AppendSystem)
	}
	for _, want := range []string{
		"project harness", "\n- api — the server\n", "\n- web\n", "\n- Billing — invoices and payments\n",
		"$HARNESS_SHELL", "README.md", ".index.json", "\n- tasks/<id>/docs/ — ", "\n- project/docs/ — ", "no task/ folder",
		"\n- mcp__task__create_task — ", `parent_id ""`, "\n- mcp__task__list_project_tasks — ",
		"\n- mcp__task__list_project_repositories — ", "\n- mcp__task__list_bounded_contexts — ", "\n- mcp__task__list_agents — ",
		"SESSION_HAS_NO_TASK", "mcp__task__get_task", "mcp__task__update_task_status", "mcp__task__request_user_review",
		"\n- mcp__task__notify — ", "\n- mcp__task__list_notifications — ",
		"\n- BASE_VERSION_STALE — ", "\n- TASK_NOT_FOUND, TASK_NOT_ON_PROJECT — ", "\n- NOT_STORED — ",
	} {
		if !strings.Contains(cfg.AppendSystem, want) {
			t.Errorf("project brief lacks %q", want)
		}
	}
	for _, unwanted := range []string{"## Your task", "\n- task/docs/ — ", "\n- task/comments/ — ", "\n- mcp__task__get_task — "} {
		if strings.Contains(cfg.AppendSystem, unwanted) {
			t.Errorf("project brief still has the task brief's %q", unwanted)
		}
	}
	if !slices.Contains(cfg.AllowedTools, "Read") {
		t.Fatal("caller-supplied allowed tools dropped")
	}
	for _, name := range tooling.SessionTaskToolNames {
		if !slices.Contains(cfg.AllowedTools, "mcp__task__"+name) {
			t.Errorf("mcp__task__%s not allow-listed", name)
		}
	}
}

// A brief with no repositories or contexts listed leaves those parts out;
// no project at all changes nothing.
func TestApplyProjectContext_Minimal(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	applyProjectContext(&cfg, projectBriefInput{Project: &domain.Project{ID: "p1", Name: "harness"}}, "")
	if len(cfg.MCPServers) != 0 || !strings.HasPrefix(cfg.AppendSystem, "## Your project") {
		t.Fatalf("minimal brief: %+v", cfg)
	}
	if strings.Contains(cfg.AppendSystem, "Repositories") || strings.Contains(cfg.AppendSystem, "Bounded contexts") {
		t.Errorf("empty lists still have headings:\n%s", cfg.AppendSystem)
	}
	none := llmkit.SessionConfig{AppendSystem: "be terse"}
	applyProjectContext(&none, projectBriefInput{}, "http://127.0.0.1:8080/mcp/task/s1")
	if len(none.MCPServers) != 0 || none.AppendSystem != "be terse" || len(none.AllowedTools) != 0 {
		t.Fatalf("no project must leave the config untouched: %+v", none)
	}
}

// The project brief is pinned byte for byte, like the peer brief: a change
// to it is deliberate. UPDATE_GOLDEN=1 rewrites the file.
func TestProjectBrief_IsUnchanged(t *testing.T) {
	got := projectBrief(projectBriefInput{
		Project:      &domain.Project{ID: "p1", Name: "harness"},
		Repositories: []*domain.Repository{{ID: "r1", Name: "api", Description: "the server"}},
		Contexts:     []*domain.BoundedContext{{ID: "bc1", Name: "Billing", Purpose: "invoices and payments"}},
	})
	const golden = "testdata/project_brief.golden"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("the project brief changed:\n%s", got)
	}
}

// pointCatalogAt makes the test catalog's project and repository the one
// the planning world (withShells) holds, so a project-level shell lists its
// tasks.
func pointCatalogAt(t *testing.T, svc *Service, projectID string) {
	t.Helper()
	repo, err := svc.catalog.Repositories.GetRepository(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	r := *repo
	r.ProjectID = projectID
	svc.catalog = catalogOf(&fakeResolver{proj: &domain.Project{ID: projectID, Name: "proj", RootDir: repo.RootDir}, repo: &r})
}

// A product session with no task gets the project's shell: the project's
// documents and design assets, every task of the project under
// tasks/<id>/docs/, and no task/ folder; it follows the project's documents
// and tasks as they change.
func TestAProductSessionWithoutATaskGetsTheProjectsShell(t *testing.T) {
	svc, _ := newInteractiveService(t)
	w := withShells(t, svc)
	pointCatalogAt(t, svc, w.task.ProjectID)
	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: w.task.ProjectID, RepositoryID: "r1", Mode: "product"})
	dir := envValue(launch.Spec.Env, ports.ShellDirEnv)
	if dir == "" || !slices.Contains(launch.Spec.AddDirs, dir) {
		t.Fatalf("no shell in the session's env %v / add dirs %v", launch.Spec.Env, launch.Spec.AddDirs)
	}
	if !strings.Contains(launch.Spec.AppendSystem, "## Your virtual shell") {
		t.Errorf("the brief does not name the shell:\n%s", launch.Spec.AppendSystem)
	}

	got := readShell(t, dir)
	plainNotes, otherNotes := w.notesA, w.notesB
	if got["tasks/"+w.task.ID+"/docs/notes.md"].content == w.notesB.Content {
		plainNotes, otherNotes = w.notesB, w.notesA
	}
	want := map[string]shellFile{
		"tasks/" + w.task.ID + "/docs/plan-rollout.html":                  {0o644, planPage},
		"tasks/" + w.task.ID + "/docs/notes.md":                           {0o644, plainNotes.Content},
		"tasks/" + w.task.ID + "/docs/notes-" + otherNotes.ID[:8] + ".md": {0o644, otherNotes.Content},
		"tasks/" + w.other.ID + "/docs/other-task-s-notes.md":             {0o644, "# Not ours\n"},
		"project/docs/glossary.md":                                        {0o644, "# Terms\n"},
		"project/docs/architecture.md":                                    {0o644, "# Layers\n"},
		"project/designs/brand/logo.svg":                                  {0o644, "<svg/>"},
		"project/designs/brand/palette.css":                               {0o644, ":root{}"},
	}
	for path, wf := range want {
		if gf, ok := got[path]; !ok || gf != wf {
			t.Errorf("%s = %+v, want %+v; shell has %v", path, gf, wf, keys(got))
		}
	}
	for _, p := range keys(got) {
		if strings.HasPrefix(p, "task/") {
			t.Errorf("a shell with no task holds %s", p)
		}
	}
	for _, p := range []string{"README.md", ".index.json", "project/designs/urls.md"} {
		if _, ok := got[p]; !ok {
			t.Errorf("%s missing", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "task")); err == nil {
		t.Error("a shell with no task has a task/ folder")
	}
	if !strings.Contains(got["README.md"].content, "no task") {
		t.Error("the README does not mention a shell with no task")
	}

	// A new task of the project and a new document on it reach the shell.
	ctx := context.Background()
	feature, err := w.plan.CreateTicket(ctx, w.task.ProjectID, "Export", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd, err := w.plan.CreateDocument(w.task.ProjectID, "PRD: Export", "# PRD: Export\n", domain.DocumentFormatMarkdown, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.plan.LinkDocument(feature.ID, prd.ID); err != nil {
		t.Fatal(err)
	}
	svc.Shells.(*vshell.Service).AnnounceTicket(feature, false)
	waitFile(t, filepath.Join(dir, "tasks", feature.ID, "docs", "prd-export.md"))

	// Ended and resumed, the session gets a fresh project shell.
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	waitGone(t, dir)
	_, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	again := readShell(t, envValue(spec.Env, ports.ShellDirEnv))
	if _, ok := again["tasks/"+feature.ID+"/docs/prd-export.md"]; !ok || len(keys(again)) == 0 {
		t.Fatalf("resumed shell: %v", keys(again))
	}
	for _, p := range keys(again) {
		if strings.HasPrefix(p, "task/") {
			t.Errorf("the resumed shell holds %s", p)
		}
	}
}
