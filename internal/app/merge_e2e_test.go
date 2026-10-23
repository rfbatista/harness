package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/domain"
)

func (f *mergeFixture) api(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Sessions: f.orch, Merges: f.merge})))
	t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, method, url string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s %s: %q: %v", method, url, raw, err)
	}
	return res.StatusCode, out
}

// Merging a session's branch from the API, against real git: a --no-ff merge
// commit on the default branch; and each way it is refused, with its own
// reason, and the checkout left alone. Origin never gets a commit.
func TestMergeASessionsBranch_EndToEnd(t *testing.T) {
	f := startMergeFixture(t)
	srv := f.api(t)
	merge := func(id string) (int, map[string]any) {
		return call(t, http.MethodPost, srv.URL+"/api/sessions/"+id+"/merge")
	}
	remote := f.remoteRefs(t)

	// A clean merge makes a merge commit, shows on the session, leaves the checkout clean.
	f.session(t, "clean", map[string]string{"clean.txt": "clean\n"})
	before := gitE2E(t, f.checkout, "rev-parse", "HEAD")
	status, body := merge("clean")
	head := gitE2E(t, f.checkout, "rev-parse", "HEAD")
	if status != http.StatusOK || body["merged"] != true || body["into"] != "main" || body["head"] != head || head == before {
		t.Fatalf("clean merge: %d %v (HEAD %s, was %s)", status, body, head, before)
	}
	if parents := strings.Fields(gitE2E(t, f.checkout, "rev-list", "--parents", "-n1", "HEAD")); len(parents) != 3 {
		t.Errorf("not a --no-ff merge commit: %v", parents)
	}
	if gitE2E(t, f.checkout, "status", "--porcelain") != "" {
		t.Error("the checkout is not clean after the merge")
	}
	if last := f.sess.Get("clean").LastMerge; last == nil || !last.Merged || last.Head != head {
		t.Errorf("recorded = %+v", last)
	}
	if tk, err := f.plan.GetTicket(context.Background(), f.task.ID); err != nil || tk.Status != domain.TicketStatusInProgress {
		t.Errorf("the task is %+v (%v): merging never moves it", tk, err)
	}

	// Merged already.
	status, body = merge("clean")
	if status != http.StatusConflict || body["code"] != domain.MergeAlreadyMerged || !strings.Contains(body["error"].(string), "already merged") {
		t.Errorf("merged twice: %d %v", status, body)
	}
	if last := f.sess.Get("clean").LastMerge; last == nil || last.Merged || last.Code != domain.MergeAlreadyMerged {
		t.Errorf("the refusal replaces the result: %+v", last)
	}

	// A dirty checkout: tracked changes, then untracked files, each said.
	f.session(t, "dirty", map[string]string{"dirty.txt": "dirty\n"})
	if err := os.WriteFile(filepath.Join(f.checkout, "README.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot(t)
	status, body = merge("dirty")
	if status != http.StatusConflict || body["code"] != domain.MergeCheckoutDirty || !strings.Contains(body["error"].(string), "uncommitted changes to tracked files (README.md)") {
		t.Errorf("tracked changes: %d %v", status, body)
	}
	if f.snapshot(t) != snap {
		t.Error("a refusal changed the dirty checkout")
	}
	gitE2E(t, f.checkout, "checkout", "--", "README.md")
	if err := os.WriteFile(filepath.Join(f.checkout, "notes.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap = f.snapshot(t)
	status, body = merge("dirty")
	if body["code"] != domain.MergeCheckoutDirty || !strings.Contains(body["error"].(string), "untracked files (notes.txt)") || strings.Contains(body["error"].(string), "uncommitted changes") {
		t.Errorf("untracked files: %d %v", status, body)
	}
	if f.snapshot(t) != snap {
		t.Error("a refusal changed the dirty checkout")
	}
	if err := os.Remove(filepath.Join(f.checkout, "notes.txt")); err != nil {
		t.Fatal(err)
	}

	// A checkout on another branch.
	gitE2E(t, f.checkout, "checkout", "-q", "-b", "elsewhere")
	snap = f.snapshot(t)
	status, body = merge("dirty")
	if status != http.StatusConflict || body["code"] != domain.MergeWrongBranch || !strings.Contains(body["error"].(string), "is on elsewhere, not on the default branch main") {
		t.Errorf("wrong branch: %d %v", status, body)
	}
	if f.snapshot(t) != snap {
		t.Error("a refusal changed the checkout")
	}
	gitE2E(t, f.checkout, "checkout", "-q", "main")

	// A conflict: aborted, HEAD, index and working tree as before.
	if err := os.WriteFile(filepath.Join(f.checkout, "shared.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, f.checkout, "add", ".")
	gitE2E(t, f.checkout, "commit", "-q", "-m", "shared")
	f.session(t, "conflict", map[string]string{"shared.txt": "from the agent\n"})
	if err := os.WriteFile(filepath.Join(f.checkout, "shared.txt"), []byte("from main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, f.checkout, "commit", "-q", "-am", "main edits shared")
	snap = f.snapshot(t)
	status, body = merge("conflict")
	if status != http.StatusConflict || body["code"] != domain.MergeConflict || !strings.Contains(body["error"].(string), "conflicts in shared.txt") {
		t.Errorf("conflict: %d %v", status, body)
	}
	if got := f.snapshot(t); got != snap {
		t.Errorf("after the conflict the checkout changed:\nbefore %s\nafter  %s", snap, got)
	}
	if _, err := os.Stat(filepath.Join(f.checkout, ".git", "MERGE_HEAD")); err == nil {
		t.Error("a merge is still in progress")
	}
	if last := f.sess.Get("conflict").LastMerge; last == nil || last.Code != domain.MergeConflict || last.Into != "main" {
		t.Errorf("recorded = %+v", last)
	}

	// A session with no branch.
	if _, err := f.sess.Create(&domain.Session{ID: "nobranch", ProjectID: f.project.ID, RepositoryID: f.repo.ID, TicketID: f.task.ID, Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}
	status, body = merge("nobranch")
	if status != http.StatusConflict || body["code"] != domain.MergeNoBranch {
		t.Errorf("no branch: %d %v", status, body)
	}
	status, body = call(t, http.MethodGet, srv.URL+"/api/sessions/nobranch/merge")
	if status != 200 || body["available"] != false || body["reason"] != domain.MergeNoBranch {
		t.Errorf("offer without a branch: %d %v", status, body)
	}
	status, body = call(t, http.MethodGet, srv.URL+"/api/sessions/clean/merge")
	if status != 200 || body["available"] != true || body["into"] != "main" {
		t.Errorf("offer: %d %v", status, body)
	}
	if status, _ = merge("nobody"); status != http.StatusNotFound {
		t.Errorf("unknown session = %d", status)
	}

	if got := f.remoteRefs(t); got != remote {
		t.Errorf("origin changed:\nbefore %s\nafter  %s", remote, got)
	}
}
