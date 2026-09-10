package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeSkill(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out struct {
		Skill map[string]any `json:"skill"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode skill: %v (body=%s)", err, body)
	}
	return out.Skill
}

func TestHTTP_SkillFileLifecycle(t *testing.T) {
	h := newMCPHandler(t)

	// create with an initial tree
	body, _ := json.Marshal(map[string]any{
		"name": "http-skill",
		"files": []map[string]any{
			{"path": "SKILL.md", "content": "---\nname: http-skill\ndescription: via http\n---\n\nbody"},
			{"path": "scripts/run.py", "content": "print(1)"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/create_skill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	skill := decodeSkill(t, rec.Body.Bytes())
	id := skill["id"].(string)
	files := skill["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("want 2 files after create, got %d", len(files))
	}

	// put a new file
	body, _ = json.Marshal(map[string]any{"skill_id": id, "path": "references/api.md", "content": "docs"})
	req = httptest.NewRequest(http.MethodPost, "/api/put_skill_file", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := len(decodeSkill(t, rec.Body.Bytes())["files"].([]any)); got != 3 {
		t.Fatalf("want 3 files after put, got %d", got)
	}

	// list files
	req = httptest.NewRequest(http.MethodGet, "/api/list_skill_files?skill_id="+id, nil)
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var listOut struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatal(err)
	}
	if len(listOut.Files) != 3 {
		t.Fatalf("want 3 files listed, got %d", len(listOut.Files))
	}

	// rename folder
	body, _ = json.Marshal(map[string]any{"skill_id": id, "old_path": "scripts", "new_path": "lib"})
	req = httptest.NewRequest(http.MethodPost, "/api/rename_skill_file", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// delete a file
	body, _ = json.Marshal(map[string]any{"skill_id": id, "path": "references/api.md"})
	req = httptest.NewRequest(http.MethodPost, "/api/delete_skill_file", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}
	files = decodeSkill(t, rec.Body.Bytes())["files"].([]any)
	for _, f := range files {
		if f.(map[string]any)["path"] == "references/api.md" {
			t.Fatal("deleted file still present")
		}
	}
}

func TestHTTP_DeleteRootSkillFileRejected(t *testing.T) {
	h := newMCPHandler(t)
	body, _ := json.Marshal(map[string]any{"name": "root-skill", "description": "d"})
	req := httptest.NewRequest(http.MethodPost, "/api/create_skill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	id := decodeSkill(t, rec.Body.Bytes())["id"].(string)

	body, _ = json.Marshal(map[string]any{"skill_id": id, "path": "SKILL.md"})
	req = httptest.NewRequest(http.MethodPost, "/api/delete_skill_file", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected error deleting root SKILL.md, got 200")
	}
}

func TestHTTP_ErrorBodyIncludesDomainCode(t *testing.T) {
	h := newMCPHandler(t)

	body, _ := json.Marshal(map[string]any{"skill_id": "nope"})
	req := httptest.NewRequest(http.MethodPost, "/api/delete_skill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] == "" {
		t.Fatal("error message missing")
	}
	if payload["code"] != "SKILL_NOT_FOUND" {
		t.Fatalf("code = %q, want SKILL_NOT_FOUND", payload["code"])
	}
}

// A rename that matches nothing is a client error about a missing resource, not
// a server fault: the repositories return SKILL_FILE_NOT_FOUND, so the status
// mapping has to carry it into the not-found set.
func TestHTTP_RenameMissingSkillFileIs404(t *testing.T) {
	h := newMCPHandler(t)

	body, _ := json.Marshal(map[string]any{
		"name": "rename-404",
		"files": []map[string]any{
			{"path": "SKILL.md", "content": "---\nname: rename-404\ndescription: d\n---\n\nbody"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/create_skill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	id := decodeSkill(t, rec.Body.Bytes())["id"].(string)

	body, _ = json.Marshal(map[string]any{
		"skill_id": id,
		"old_path": "does/not/exist.md",
		"new_path": "somewhere.md",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/rename_skill_file", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("rename of a missing file: status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errBody.Code != "SKILL_FILE_NOT_FOUND" {
		t.Fatalf("error code = %q, want SKILL_FILE_NOT_FOUND", errBody.Code)
	}
}

// Renaming a skill over the API must not touch its tree. The UI sends the name
// on every edit, which is exactly the shape that used to replace the whole
// tree with a bare SKILL.md.
func TestHTTP_RenameSkillKeepsItsFiles(t *testing.T) {
	h := newMCPHandler(t)
	post := func(t *testing.T, route string, payload map[string]any) map[string]any {
		t.Helper()
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/"+route, bytes.NewReader(body))
		rec := httptest.NewRecorder()
		NewRouter(h).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", route, rec.Code, rec.Body.String())
		}
		return decodeSkill(t, rec.Body.Bytes())
	}

	created := post(t, "create_skill", map[string]any{
		"name":        "before-rename",
		"description": "has a tree",
		"files": []map[string]any{
			{"path": "SKILL.md", "content": "---\nname: before-rename\ndescription: has a tree\n---\n\nthe body"},
			{"path": "scripts/run.py", "content": "print(1)"},
			{"path": "references/api.md", "content": "docs"},
		},
	})
	id := created["id"].(string)

	updated := post(t, "update_skill", map[string]any{
		"skill_id": id,
		"name":     "after-rename",
	})
	if updated["name"] != "after-rename" {
		t.Errorf("name = %v, want after-rename", updated["name"])
	}

	paths := map[string]bool{}
	for _, f := range updated["files"].([]any) {
		paths[f.(map[string]any)["path"].(string)] = true
	}
	for _, want := range []string{"SKILL.md", "scripts/run.py", "references/api.md"} {
		if !paths[want] {
			t.Errorf("%q lost by the rename; kept %v", want, paths)
		}
	}

	// and it must survive a re-read, not just the update's response
	req := httptest.NewRequest(http.MethodGet, "/api/get_skill?skill_id="+id, nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get_skill status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := len(decodeSkill(t, rec.Body.Bytes())["files"].([]any)); got != 3 {
		t.Fatalf("re-read shows %d files, want 3", got)
	}
}
