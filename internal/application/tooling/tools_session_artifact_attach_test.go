package tooling

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// asset publishes path from sess-1 (the card task) and moves it to the project.
func (f *artifactToolsFixture) asset(t *testing.T, path string) string {
	t.Helper()
	id := f.publish(t, "sess-1", f.root, path)
	if _, err := f.art.SetArtifactScope(context.Background(), id, domain.ArtifactScopeProject); err != nil {
		t.Fatal(err)
	}
	return id
}

// attachedTo is the tasks the asset is attached to, as the service lists
// them on the producing task.
func (f *artifactToolsFixture) attachedTo(t *testing.T, id string) []string {
	t.Helper()
	list, err := f.art.ListTaskArtifacts(context.Background(), f.ticketID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.ID == id {
			return a.AttachedTicketIDs
		}
	}
	t.Fatalf("%s is not listed on its task", id)
	return nil
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
	if got := f.attachedTo(t, logo); strings.Join(got, ",") != f.otherTicketID {
		t.Fatalf("attached to %v", got)
	}

	out, err = f.call(t, "sess-3", "detach_artifact_from_task", map[string]any{"artifact_id": logo})
	if err != nil {
		t.Fatal(err)
	}
	if got := out["artifact"].(projectArtifact); got.AttachedTicketIDs == nil || len(got.AttachedTicketIDs) != 0 {
		t.Fatalf("detached = %+v", got)
	}
	if got := f.attachedTo(t, logo); len(got) != 0 {
		t.Fatalf("still attached to %v", got)
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
