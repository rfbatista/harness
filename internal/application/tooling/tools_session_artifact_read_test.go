package tooling

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// readArtifact calls read_artifact as session and answers the JSON an agent sees.
func (f *artifactToolsFixture) readArtifact(t *testing.T, session string, args map[string]any) map[string]any {
	t.Helper()
	out, err := f.call(t, session, "read_artifact", args)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func (f *artifactToolsFixture) publishFile(t *testing.T, session, root, path, content string) string {
	t.Helper()
	f.write(t, root, path, content)
	out, err := f.call(t, session, "publish_artifact", map[string]any{"path": path, "title": path})
	if err != nil {
		t.Fatal(err)
	}
	return out["artifact_id"].(string)
}

func TestReadArtifact_TextAssetsReadLikeDocuments(t *testing.T) {
	f := newArtifactToolsFixture(t)
	page := "<!doctype html><html><head><style>.x{}</style></head><body><h1>Card</h1><p>price</p></body></html>"
	id := f.publishFile(t, "sess-1", f.root, "design/card.html", page)
	whole := f.readArtifact(t, "sess-1", map[string]any{"artifact_id": id})
	a := whole["artifact"].(map[string]any)
	if a["content"] != page || a["lines"] != float64(2) || a["version"] != domain.DocumentVersion(page) || a["kind"] != "page" || a["revision"] != float64(1) {
		t.Fatalf("small page: %v", a)
	}

	svg := `<svg xmlns="http://www.w3.org/2000/svg"><text>logo</text></svg>`
	sid := f.publishFile(t, "sess-1", f.root, "logo.svg", svg)
	if got := f.readArtifact(t, "sess-1", map[string]any{"artifact_id": sid, "view": "raw"}); len(got["lines"].([]any)) != 1 ||
		got["lines"].([]any)[0].(map[string]any)["text"] != svg {
		t.Fatalf("svg raw: %v", got)
	}

	var rows []string
	for i := 0; i < 500; i++ {
		rows = append(rows, `  {"token": "color-`+strings.Repeat("x", 3)+`", "value": 1},`)
	}
	jid := f.publishFile(t, "sess-1", f.root, "tokens.json", "[\n"+strings.Join(rows, "\n")+"\n]\n")
	big := f.readArtifact(t, "sess-1", map[string]any{"artifact_id": jid})
	if _, ok := big["artifact"].(map[string]any)["content"]; ok || big["truncated"] != true || len(big["lines"].([]any)) != 200 || big["next_offset"] != float64(201) {
		t.Fatalf("a 502-line JSON over the guard: truncated %v next %v", big["truncated"], big["next_offset"])
	}
	win := f.readArtifact(t, "sess-1", map[string]any{"artifact_id": jid, "offset": 502})
	if l := win["lines"].([]any); len(l) != 1 || l[0].(map[string]any)["text"] != "]" {
		t.Fatalf("last line: %v", l)
	}
}

func TestReadArtifact_BinaryLargeAndURL(t *testing.T) {
	f := newArtifactToolsFixture(t)
	png := f.publishFile(t, "sess-1", f.root, "shot.png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	got := f.readArtifact(t, "sess-1", map[string]any{"artifact_id": png})
	if _, ok := got["lines"]; ok || got["view_url"] != "/api/artifacts/"+png+"/view/" || !strings.Contains(got["note"].(string), "not read as text") {
		t.Fatalf("png: %v", got)
	}
	huge := f.publishFile(t, "sess-1", f.root, "huge.txt", strings.Repeat("a\n", maxArtifactText/2+1))
	got = f.readArtifact(t, "sess-1", map[string]any{"artifact_id": huge})
	if _, ok := got["lines"]; ok || got["view_url"] == nil || !strings.Contains(got["note"].(string), "2 MiB") {
		t.Fatalf("over 2 MiB: %v", got)
	}
	link, err := f.call(t, "sess-1", "publish_artifact", map[string]any{"url": "http://localhost:5173/", "title": "Dev"})
	if err != nil {
		t.Fatal(err)
	}
	got = f.readArtifact(t, "sess-1", map[string]any{"artifact_id": link["artifact_id"]})
	if got["url"] != "http://localhost:5173/" || got["lines"] != nil {
		t.Fatalf("url artifact: %v", got)
	}
}

// read_artifact sees what this task produced, what is attached to it and the
// project's assets; not another task's own artifacts, not another project's.
func TestReadArtifact_Visibility(t *testing.T) {
	f := newArtifactToolsFixture(t)
	mineOther := f.publish(t, "sess-2", f.root2, "peer.html") // another session, same task
	theirs := f.publish(t, "sess-3", f.root3, "theirs.html")  // another task, task scope
	asset := f.publish(t, "sess-3", f.root3, "asset.html")    // another task, moved to the project
	f.moved(t, "sess-3", "move_artifact_to_project", asset)
	foreign := f.publish(t, "sess-4", f.root4, "foreign.html") // another project

	for _, id := range []string{mineOther, asset} {
		if got := f.readArtifact(t, "sess-1", map[string]any{"artifact_id": id}); got["artifact"].(map[string]any)["content"] == nil {
			t.Fatalf("%s is not readable: %v", id, got)
		}
	}
	for id, code := range map[string]string{theirs: "ARTIFACT_NOT_FOUND", foreign: "ARTIFACT_PROJECT_MISMATCH", "missing": "ARTIFACT_NOT_FOUND", "": "INVALID_INPUT"} {
		_, err := f.call(t, "sess-1", "read_artifact", map[string]any{"artifact_id": id})
		wantCode(t, err, code)
		if !strings.HasPrefix(err.(*domain.StructuredError).Message, code+": ") {
			t.Fatalf("not coded: %v", err)
		}
	}
	// A task artifact whose worktree file is gone says so, coded.
	if err := os.Remove(filepath.Join(f.root2, "peer.html")); err != nil {
		t.Fatal(err)
	}
	_, err := f.call(t, "sess-1", "read_artifact", map[string]any{"artifact_id": mineOther})
	wantCode(t, err, "ARTIFACT_NOT_FOUND")
	if msg := err.(*domain.StructuredError).Message; !strings.HasPrefix(msg, "ARTIFACT_NOT_FOUND: ") || strings.Count(msg, "ARTIFACT_NOT_FOUND") != 1 {
		t.Fatalf("gone file: %v", err)
	}
}

// search_documents can include the text assets read_artifact sees, and opens
// at most 50 of them.
func TestSearchDocuments_IncludesArtifacts(t *testing.T) {
	f := newArtifactToolsFixture(t)
	page := f.publishFile(t, "sess-1", f.root, "card.html", "<h1>Card</h1><p>needle price</p>")
	f.publishFile(t, "sess-1", f.root, "shot.png", "\x89PNG\r\n\x1a\nneedle")
	theirs := f.publishFile(t, "sess-3", f.root3, "theirs.html", "<p>needle theirs</p>")

	out, err := f.call(t, "sess-1", "search_documents", map[string]any{"pattern": "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if ms := out["matches"].([]searchMatch); len(ms) != 0 {
		t.Fatalf("artifacts searched without include_artifacts: %+v", ms)
	}
	out, err = f.call(t, "sess-1", "search_documents", map[string]any{"pattern": "needle", "include_artifacts": true})
	if err != nil {
		t.Fatal(err)
	}
	ms := out["matches"].([]searchMatch)
	if len(ms) != 1 || ms[0].Source != "artifact" || ms[0].ID != page || ms[0].Text != "needle price" || ms[0].SectionID != "1" || out["searched"] != 1 {
		t.Fatalf("with artifacts: %+v %v", ms, out)
	}
	for _, m := range ms {
		if m.ID == theirs {
			t.Fatal("another task's artifact was searched")
		}
	}

	for i := 0; i < maxSearchedArtifacts+3; i++ {
		f.publishFile(t, "sess-1", f.root, "many/"+strings.Repeat("a", i+1)+".txt", "hay")
	}
	out, err = f.call(t, "sess-1", "search_documents", map[string]any{"pattern": "hay", "include_artifacts": true, "max_matches": 200})
	if err != nil {
		t.Fatal(err)
	}
	if out["searched"] != maxSearchedArtifacts || out["skipped"] != 4 || len(out["matches"].([]searchMatch)) != maxSearchedArtifacts {
		t.Fatalf("cap: searched %v skipped %v matches %d", out["searched"], out["skipped"], len(out["matches"].([]searchMatch)))
	}
}
