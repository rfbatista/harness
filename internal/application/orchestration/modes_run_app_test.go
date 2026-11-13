package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

var runAppReferenceFiles = []string{"ENGINES.md", "K3S-SHAPES.md", "SEED-FORMATS.md"}

func requireAll(t *testing.T, name, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s lacks %q", name, w)
		}
	}
}

// Only an architect session carries task-run-app, with its reference files.
func TestStartInteractive_ArchitectCarriesTaskRunApp(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	body := pluginSkill(t, launch, "task-run-app")
	requireAll(t, "task-run-app SKILL.md", body, "name: task-run-app", "description:", "ask_person")
	if strings.Contains(body, "AskUserQuestion") {
		t.Error("task-run-app SKILL.md names a provider tool; it must ask through ask_person")
	}
	for _, f := range runAppReferenceFiles {
		pluginFile(t, launch, "task-run-app", f)
		if !strings.Contains(body, f) {
			t.Errorf("task-run-app SKILL.md does not point at %s", f)
		}
	}
	// The person invokes it; the architect's first message is unchanged.
	desc := body[:strings.Index(body[3:], "---")+3]
	requireAll(t, "task-run-app description", desc, "person", "never")
	if strings.Contains(launch.Spec.Prompt, "task-run-app") {
		t.Errorf("first message names task-run-app: %q", launch.Spec.Prompt)
	}
}

func TestStartInteractive_OtherModesDoNotCarryTaskRunApp(t *testing.T) {
	svc, _ := newInteractiveService(t)
	for _, mode := range []string{"implement", "design", "product", ""} {
		_, launch := startInteractive(t, svc, InteractiveRequest{Mode: mode})
		dir, ok := flag(launch.Args, "--plugin-dir")
		if !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "skills", "task-run-app")); err == nil {
			t.Errorf("mode %q carries task-run-app", mode)
		}
	}
}

func TestResumeInteractive_KeepsTaskRunApp(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	pluginSkill(t, launchOf(t, spec), "task-run-app")
}

// The database half: engine, k3s, naming, and what it never touches.
func TestTaskRunApp_PreparesTheTaskDatabase(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	body := pluginSkill(t, launch, "task-run-app")
	requireAll(t, "task-run-app SKILL.md", body,
		"grep -n", "sed -n", "never the whole",
		"k3s", "pods_exec", "task database", "NodePort", "scenario", "SQLite",
		"task_<task short id>", "task-<task short id>", "<engine>-nodeport-task-<task short id>",
		"never delete", "never scale", "never replace",
		"no `k3s`", "no `services`", "no branch",
	)
	engines := pluginFile(t, launch, "task-run-app", "ENGINES.md")
	requireAll(t, "ENGINES.md", engines, ".env.example", "DATABASE_URL", "DB_DRIVER", "postgres", "mysql", "sqlite")
	shapes := pluginFile(t, launch, "task-run-app", "K3S-SHAPES.md")
	requireAll(t, "K3S-SHAPES.md", shapes, "NodePort", "Secret", "InternalIP", "Deployment", "task-<task short id>")
}

// The service half: the four outputs, both servers, the fixed tool arguments.
func TestTaskRunApp_RunsTheTaskServiceAndReports(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	body := pluginSkill(t, launch, "task-run-app")
	requireAll(t, "task-run-app SKILL.md", body,
		"task service", "task database", "Seed document", "task comment",
		"`services`", "`k3s`",
		"create_service", "export_repository_env", "update_service", "start_service", "restart_service",
		`worktree: "branch:<name>"`, `"branch:origin/<name>"`,
		"reason", "blocks until the person answers", "do not retry",
		"<repository>-<task short id>", ".env.example", "task/comments/", "task/docs/", "Seed: <task title>",
		"overwrite", "read_service_logs", "SERVICE_REQUEST_DENIED",
		"Refusals", "Rerun",
	)
	formats := pluginFile(t, launch, "task-run-app", "SEED-FORMATS.md")
	requireAll(t, "SEED-FORMATS.md", formats, "Seed: <task title>", "scenario", "Test credentials", "password")
}

// The schema comes from the branch, the seed after it, SQLite through the service.
func TestTaskRunApp_SchemaComesFromTheBranchBeforeTheSeed(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect"})
	body := pluginSkill(t, launch, "task-run-app")
	requireAll(t, "task-run-app SKILL.md", body,
		"Never hand-write a schema", "boot migrations", "delegate session worktree",
		"SEED_SQL", "Never write into the repository or any worktree.",
		"recommending the one in namespace `default`", "in your own worktree",
	)
	if strings.Contains(body, "is the one exception") {
		t.Error("task-run-app SKILL.md still lets the skill write a SQLite file into a worktree")
	}
	order := []string{"### 6. Create or update the task service", "### 7. Start it on the branch", "### 8. Get the schema from the branch", "### 9. Seed", "### 10. Verify"}
	last := -1
	for _, h := range order {
		i := strings.Index(body, h)
		if i < 0 || i < last {
			t.Fatalf("heading %q missing or out of order", h)
		}
		last = i
	}
	requireAll(t, "ENGINES.md", pluginFile(t, launch, "task-run-app", "ENGINES.md"), "SEED_SQL", `sqlite3 "$DB_PATH"`)
	requireAll(t, "SEED-FORMATS.md", pluginFile(t, launch, "task-run-app", "SEED-FORMATS.md"), "boot migrations")
}
