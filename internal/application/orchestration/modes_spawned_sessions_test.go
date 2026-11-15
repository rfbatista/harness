package orchestration

import (
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// ADR 0093: running and QA of the application always run in spawned sessions.
// These tests assert the skill files and opening messages a session receives.

// guardBefore checks the skill's first section is the guard, ahead of any step.
func guardBefore(t *testing.T, name, body, firstStep string, wants ...string) {
	t.Helper()
	guard := strings.Index(body, "## Before you start")
	step := strings.Index(body, firstStep)
	if guard < 0 || step < 0 || guard > step {
		t.Fatalf("%s: the guard section is missing or does not come before %q", name, firstStep)
	}
	requireAll(t, name+" guard", body[guard:step], wants...)
}

func TestTaskRunApp_OutsideOperateModeOnlySpawnsAnOperator(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "operate"})
	body := pluginSkill(t, launch, "task-run-app")
	guardBefore(t, "task-run-app", body, "## Rules that always hold",
		"not in operate mode", "run no step", "start_task_session", `mode: "operate"`, "person's request", "Wait for the Operator", "report")
	desc := body[:strings.Index(body[3:], "---")+3]
	requireAll(t, "task-run-app description", desc, "Outside operate mode", "only spawns")
}

func TestTaskRunApp_ResultsLiveInTheSeedDocumentAndNoCommentCarriesCredentials(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "operate"})
	body := pluginSkill(t, launch, "task-run-app")
	requireAll(t, "task-run-app SKILL.md", body,
		"credentials into the Seed document", "Write no task comment", "Seed document attached", ".index.json",
		"no architect", "pointer comment", "without the credentials", "creates no new file")
	for _, gone := range []string{"`run-app.md`", "handed over in a **task comment**"} {
		if strings.Contains(body, gone) {
			t.Errorf("task-run-app SKILL.md still has %q", gone)
		}
	}
	formats := pluginFile(t, launch, "task-run-app", "SEED-FORMATS.md")
	requireAll(t, "SEED-FORMATS.md", formats, "## Test credentials", "Seed: <task title>", "password")
	for _, gone := range []string{"run-app.md", "## Task comment"} {
		if strings.Contains(formats, gone) {
			t.Errorf("SEED-FORMATS.md still has %q", gone)
		}
	}
}

func TestTaskOperateAndItsOpeningAgreeOnWhereResultsGo(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Roles = &fakeRoles{roles: map[string]domain.SessionRole{"arch": domain.RoleArchitect}}
	_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "operate", ParentSessionID: "arch"})
	body := pluginSkill(t, launch, "task-operate")
	requireAll(t, "task-operate SKILL.md", body, "Seed document", "task-run-app")
	if strings.Contains(body, "run-app.md") {
		t.Error("task-operate names the run-app comment")
	}
	requireAll(t, "operate opening", launch.Spec.Prompt, "Seed document", "Seed document attached")
	if strings.Contains(launch.Spec.Prompt, "task comment") && strings.Contains(launch.Spec.Prompt, "credentials in a") {
		t.Errorf("operate opening hands credentials over in a comment: %q", launch.Spec.Prompt)
	}
}
