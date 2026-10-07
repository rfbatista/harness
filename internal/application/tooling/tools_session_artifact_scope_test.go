package tooling

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// publish publishes path from session's worktree and returns its artifact_id;
// a failure ends the test.
func (f *artifactToolsFixture) publish(t *testing.T, session, root, path string) string {
	t.Helper()
	f.write(t, root, path, "<h1>"+path+"</h1>")
	out, err := f.call(t, session, "publish_artifact", map[string]any{"path": path, "title": path})
	if err != nil {
		t.Fatal(err)
	}
	return out["artifact_id"].(string)
}

// moved calls a move tool as session and returns the artifact it answers.
func (f *artifactToolsFixture) moved(t *testing.T, session, tool, id string) projectArtifact {
	t.Helper()
	out, err := f.call(t, session, tool, map[string]any{"artifact_id": id})
	if err != nil {
		t.Fatal(err)
	}
	return out["artifact"].(projectArtifact)
}

// projectAssets is list_project_artifacts as session.
func (f *artifactToolsFixture) projectAssets(t *testing.T, session string) []projectArtifact {
	t.Helper()
	out, err := f.call(t, session, "list_project_artifacts", nil)
	if err != nil {
		t.Fatal(err)
	}
	list := out["artifacts"].([]projectArtifact)
	if out["count"] != len(list) {
		t.Fatalf("count %v for %d artifacts", out["count"], len(list))
	}
	return list
}

func (f *artifactToolsFixture) taskScopeOf(t *testing.T, session, id string) domain.ArtifactScope {
	t.Helper()
	out, err := f.call(t, session, "list_task_artifacts", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range out["artifacts"].([]taskArtifact) {
		if a.ArtifactID == id {
			return a.Scope
		}
	}
	t.Fatalf("%s is not listed on its task", id)
	return ""
}

// What a task's session moves to the project, the project lists with its
// task and view URL; the task keeps listing it; moving again writes nothing.
func TestArtifactScopeTools_MoveToProjectAndBack(t *testing.T) {
	f := newArtifactToolsFixture(t)
	id := f.publish(t, "sess-1", f.root, "design/logo.html")
	if got := f.taskScopeOf(t, "sess-1", id); got != domain.ArtifactScopeTask {
		t.Fatalf("a new artifact is scope %q, want task", got)
	}
	if list := f.projectAssets(t, "sess-1"); len(list) != 0 {
		t.Fatalf("a task artifact is listed as a project asset: %+v", list)
	}

	time.Sleep(2 * time.Millisecond)
	moved := f.moved(t, "sess-1", "move_artifact_to_project", id)
	if moved.ArtifactID != id || moved.Scope != domain.ArtifactScopeProject || moved.ViewURL != "/api/artifacts/"+id+"/view/" ||
		moved.TaskID != f.ticketID || moved.TaskTitle != "Design the card" || moved.Kind != domain.ArtifactPage || moved.Revision != 1 {
		t.Fatalf("move = %+v", moved)
	}
	list := f.projectAssets(t, "sess-1")
	if len(list) != 1 || list[0].ArtifactID != id || list[0].Scope != domain.ArtifactScopeProject || list[0].ViewURL != moved.ViewURL ||
		list[0].TaskTitle != moved.TaskTitle || !list[0].UpdatedAt.Equal(moved.UpdatedAt) {
		t.Fatalf("project listing = %+v, want %+v", list, moved)
	}
	if got := f.taskScopeOf(t, "sess-1", id); got != domain.ArtifactScopeProject {
		t.Fatalf("the task lists its moved artifact as %q", got)
	}

	again := f.moved(t, "sess-1", "move_artifact_to_project", id)
	if !again.UpdatedAt.Equal(moved.UpdatedAt) {
		t.Fatalf("idempotent move wrote: %v then %v", moved.UpdatedAt, again.UpdatedAt)
	}

	back := f.moved(t, "sess-1", "move_artifact_to_task", id)
	if back.Scope != domain.ArtifactScopeTask || back.ArtifactID != id {
		t.Fatalf("move back = %+v", back)
	}
	if list := f.projectAssets(t, "sess-1"); len(list) != 0 {
		t.Fatalf("still a project asset: %+v", list)
	}
}

// Any session on the task moves what was published on it, not only its own.
func TestArtifactScopeTools_AnySessionOnTheTaskMoves(t *testing.T) {
	f := newArtifactToolsFixture(t)
	id := f.publish(t, "sess-1", f.root, "palette.html")
	if got := f.moved(t, "sess-2", "move_artifact_to_project", id); got.Scope != domain.ArtifactScopeProject {
		t.Fatalf("move by a peer = %+v", got)
	}
}

// A session moves only what was published on its own task: not another
// task's artifacts, not another project's, nothing by a missing id.
func TestArtifactScopeTools_MoveNeedsThisTask(t *testing.T) {
	f := newArtifactToolsFixture(t)
	sibling := f.publish(t, "sess-3", f.root3, "theirs.html")
	foreign := f.publish(t, "sess-4", f.root4, "foreign.html")

	for _, name := range []string{"move_artifact_to_project", "move_artifact_to_task"} {
		for _, id := range []string{sibling, foreign, "missing"} {
			_, err := f.call(t, "sess-1", name, map[string]any{"artifact_id": id})
			wantCode(t, err, "ARTIFACT_NOT_ON_TASK")
			if !strings.HasPrefix(err.Error(), "ARTIFACT_NOT_ON_TASK: ") {
				t.Fatalf("%s: the code is not in the message: %v", name, err)
			}
		}
		_, err := f.call(t, "sess-1", name, map[string]any{})
		wantCode(t, err, "INVALID_INPUT")
		_, err = f.call(t, "", name, map[string]any{"artifact_id": sibling})
		wantCode(t, err, "SESSION_NOT_FOUND")
	}
	if got := f.taskScopeOf(t, "sess-3", sibling); got != domain.ArtifactScopeTask {
		t.Fatalf("a refused move changed the artifact: %q", got)
	}
}

// A dev-server URL dies with its session, so it cannot become a project asset.
func TestArtifactScopeTools_URLIsNotPromotable(t *testing.T) {
	f := newArtifactToolsFixture(t)
	out, err := f.call(t, "sess-1", "publish_artifact", map[string]any{"url": "http://localhost:5173/", "title": "Dev"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.call(t, "sess-1", "move_artifact_to_project", map[string]any{"artifact_id": out["artifact_id"]})
	wantCode(t, err, "ARTIFACT_NOT_PROMOTABLE")
	if !strings.HasPrefix(err.Error(), "ARTIFACT_NOT_PROMOTABLE: ") {
		t.Fatalf("the code is not in the message: %v", err)
	}
}

// The project's assets come from any of its tasks, never from another
// project, and a task-scoped artifact is not one of them.
func TestArtifactScopeTools_ProjectListIsProjectWide(t *testing.T) {
	f := newArtifactToolsFixture(t)
	logo := f.publish(t, "sess-3", f.root3, "logo.html")
	f.moved(t, "sess-3", "move_artifact_to_project", logo)
	f.publish(t, "sess-3", f.root3, "draft.html")
	foreign := f.publish(t, "sess-4", f.root4, "foreign.html")
	f.moved(t, "sess-4", "move_artifact_to_project", foreign)

	list := f.projectAssets(t, "sess-1")
	if len(list) != 1 || list[0].ArtifactID != logo || list[0].TaskTitle != "Design the logo" {
		t.Fatalf("project listing for another task's session = %+v", list)
	}
}

// A session cannot take down what the project keeps; the error says how.
func TestArtifactScopeTools_UnpublishProjectAssetSaysMoveBack(t *testing.T) {
	f := newArtifactToolsFixture(t)
	id := f.publish(t, "sess-1", f.root, "logo.html")
	f.moved(t, "sess-1", "move_artifact_to_project", id)

	_, err := f.call(t, "sess-1", "unpublish_artifact", map[string]any{"artifact_id": id})
	wantCode(t, err, "ARTIFACT_IN_PROJECT")
	if !strings.HasPrefix(err.Error(), "ARTIFACT_IN_PROJECT: ") || !strings.Contains(err.Error(), "move_artifact_to_task") {
		t.Fatalf("the message does not say how to proceed: %v", err)
	}

	f.moved(t, "sess-1", "move_artifact_to_task", id)
	if out, err := f.call(t, "sess-1", "unpublish_artifact", map[string]any{"artifact_id": id}); err != nil || out["removed"] != true {
		t.Fatalf("unpublish after moving back = %+v, %v", out, err)
	}
}

// Like every task tool, they act on the session's own task and project.
func TestArtifactScopeTools_TakeNoProjectOrTaskID(t *testing.T) {
	f := newArtifactToolsFixture(t)
	for _, name := range []string{"list_project_artifacts", "move_artifact_to_project", "move_artifact_to_task"} {
		schema, _ := json.Marshal(f.tools[name].InputSchema)
		for _, field := range []string{"project_id", "task_id", "ticket_id"} {
			if strings.Contains(string(schema), `"`+field+`"`) {
				t.Errorf("%s takes %s: %s", name, field, schema)
			}
		}
	}
}
