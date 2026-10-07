package tooling

import (
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// asset publishes path from sess-1 (the card task) and moves it to the project.
func (f *artifactToolsFixture) asset(t *testing.T, path string) string {
	t.Helper()
	id := f.publish(t, "sess-1", f.root, path)
	f.moved(t, "sess-1", "move_artifact_to_project", id)
	return id
}

func (f *artifactToolsFixture) taskArtifacts(t *testing.T, session string) map[string]taskArtifact {
	t.Helper()
	out, err := f.call(t, session, "list_task_artifacts", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]taskArtifact{}
	for _, a := range out["artifacts"].([]taskArtifact) {
		if a.AttachedTicketIDs == nil {
			t.Fatalf("%s: attached_ticket_ids is nil", a.ArtifactID)
		}
		rows[a.ArtifactID] = a
	}
	return rows
}

// A session attaches a project asset of another task to its own task: its
// listing shows it as attached, the producer's as produced, and the project's
// listing names the task it is attached to.
func TestAttachArtifactToTask_ShowsOnBothTasks(t *testing.T) {
	f := newArtifactToolsFixture(t)
	logo := f.asset(t, "logo.html")

	out, err := f.call(t, "sess-3", "attach_artifact_to_task", map[string]any{"artifact_id": logo})
	if err != nil {
		t.Fatal(err)
	}
	got := out["artifact"].(projectArtifact)
	if got.ArtifactID != logo || strings.Join(got.AttachedTicketIDs, ",") != f.otherTicketID || got.TaskID != f.ticketID || got.TaskTitle != "Design the card" {
		t.Fatalf("attached = %+v", got)
	}
	if row, ok := f.taskArtifacts(t, "sess-3")[logo]; !ok || row.Relation != "attached" {
		t.Fatalf("the attaching task lists %+v", row)
	}
	row := f.taskArtifacts(t, "sess-1")[logo]
	if row.Relation != "produced" || strings.Join(row.AttachedTicketIDs, ",") != f.otherTicketID {
		t.Fatalf("the producing task lists %+v", row)
	}
	if list := f.projectAssets(t, "sess-2"); len(list) != 1 || strings.Join(list[0].AttachedTicketIDs, ",") != f.otherTicketID {
		t.Fatalf("project assets = %+v", list)
	}

	out, err = f.call(t, "sess-3", "detach_artifact_from_task", map[string]any{"artifact_id": logo})
	if err != nil {
		t.Fatal(err)
	}
	if got := out["artifact"].(projectArtifact); got.AttachedTicketIDs == nil || len(got.AttachedTicketIDs) != 0 {
		t.Fatalf("detached = %+v", got)
	}
	if _, ok := f.taskArtifacts(t, "sess-3")[logo]; ok {
		t.Fatal("the detached task still lists the asset")
	}
}

// An attached asset is not this task's to move: the move tools keep acting
// only on what the task produced.
func TestAttachedArtifact_IsNotTheTasksToMove(t *testing.T) {
	f := newArtifactToolsFixture(t)
	logo := f.asset(t, "logo.html")
	if _, err := f.call(t, "sess-3", "attach_artifact_to_task", map[string]any{"artifact_id": logo}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"move_artifact_to_task", "move_artifact_to_project"} {
		_, err := f.call(t, "sess-3", name, map[string]any{"artifact_id": logo})
		wantCode(t, err, "ARTIFACT_NOT_ON_TASK")
	}
	if got := f.taskScopeOf(t, "sess-1", logo); got != domain.ArtifactScopeProject {
		t.Fatalf("a refused move changed the asset: %q", got)
	}
}

func TestAttachDetachTools_ErrorsCarryTheirCode(t *testing.T) {
	f := newArtifactToolsFixture(t)
	logo := f.asset(t, "logo.html")
	draft := f.publish(t, "sess-1", f.root, "draft.html")

	for _, c := range []struct {
		session, tool, artifact, code string
	}{
		{"sess-3", "attach_artifact_to_task", "", "INVALID_INPUT"},
		{"sess-3", "detach_artifact_from_task", "", "INVALID_INPUT"},
		{"sess-3", "attach_artifact_to_task", "missing", "ARTIFACT_NOT_FOUND"},
		{"sess-4", "attach_artifact_to_task", logo, "ARTIFACT_PROJECT_MISMATCH"},
		{"sess-3", "attach_artifact_to_task", draft, "ARTIFACT_NOT_IN_PROJECT"},
		{"sess-1", "detach_artifact_from_task", logo, "ARTIFACT_PRODUCER_TASK"},
		{"", "attach_artifact_to_task", logo, "SESSION_NOT_FOUND"},
	} {
		_, err := f.call(t, c.session, c.tool, map[string]any{"artifact_id": c.artifact})
		wantCode(t, err, c.code)
		if strings.HasPrefix(c.code, "ARTIFACT_") && !strings.HasPrefix(err.Error(), c.code+": ") {
			t.Errorf("%s %s: the code is not in the message: %v", c.tool, c.artifact, err)
		}
	}
	// Attaching to the producing task, or attaching twice, changes nothing.
	for _, s := range []string{"sess-1", "sess-3", "sess-3"} {
		if _, err := f.call(t, s, "attach_artifact_to_task", map[string]any{"artifact_id": logo}); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func TestAttachDetachTools_AreAllowListed(t *testing.T) {
	names := strings.Join(SessionTaskToolNames, ",")
	for _, want := range []string{"attach_artifact_to_task", "detach_artifact_from_task"} {
		if !strings.Contains(names, want) {
			t.Errorf("%s is not in SessionTaskToolNames", want)
		}
	}
}
