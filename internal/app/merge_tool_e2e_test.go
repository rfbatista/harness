package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// mergeTool is merge_session_branch, called as one session of the fixture's
// project, the way its per-session MCP endpoint does.
func (f *mergeFixture) mergeTool(t *testing.T, as, target string) (map[string]any, error) {
	t.Helper()
	var tool *domain.Tool
	for _, tl := range sessionTaskTools(f.cat, f.orch, f.plan, f.sess, nil, f.channel, nil, f.merge) {
		if tl.Name == "merge_session_branch" {
			tool = &tl
		}
	}
	if tool == nil {
		t.Fatal("merge_session_branch is not served by the task server")
	}
	res, err := tool.Handler(tooling.WithSessionID(context.Background(), as), map[string]any{"session_id": target})
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out, nil
}

// review opens a review about the delegate for the architect and, when
// decision is not pending, has the person settle it.
func (f *mergeFixture) review(t *testing.T, architect, about string, decision domain.ReviewState) {
	t.Helper()
	ctx := context.Background()
	r, err := f.channel.RequestUserReview(ctx, architect, ports.ReviewRequestInput{Subject: "Review " + about, Body: "please look", AboutSessionID: about})
	if err != nil {
		t.Fatal(err)
	}
	if decision == domain.ReviewPending {
		return
	}
	if _, _, err := f.channel.RespondReview(ctx, r.ID, decision, "see the diff"); err != nil {
		t.Fatal(err)
	}
}

func (f *mergeFixture) architect(t *testing.T, id string, task *domain.Ticket) {
	t.Helper()
	if _, err := f.sess.Create(&domain.Session{ID: id, ProjectID: f.project.ID, RepositoryID: f.repo.ID, TicketID: task.ID, Mode: domain.SessionModeArchitect, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
}

func wantRefusal(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("not refused; want %v", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("refusal %q lacks %q", err, p)
		}
	}
}

// An architect merges a delegate's branch with merge_session_branch once the
// person approved a review about that delegate, against real git; each
// refusal says why in its message and leaves the checkout alone; and what it
// records on the session is what the web action records.
func TestArchitectMergesADelegatesBranch_EndToEnd(t *testing.T) {
	f := startMergeFixture(t)
	f.architect(t, "arch", f.task)
	remote := f.remoteRefs(t)

	f.session(t, "delegate", map[string]string{"delegate.txt": "work\n"})

	// No review request about it, then one the person has not answered, then
	// one that asked for changes: each refused, and the checkout untouched.
	snap := f.snapshot(t)
	_, err := f.mergeTool(t, "arch", "delegate")
	wantRefusal(t, err, "MERGE_NOT_APPROVED", "no review request")
	f.review(t, "arch", "delegate", domain.ReviewPending)
	_, err = f.mergeTool(t, "arch", "delegate")
	wantRefusal(t, err, "MERGE_NOT_APPROVED", "has not answered")
	f.session(t, "revised", map[string]string{"revised.txt": "work\n"})
	f.review(t, "arch", "revised", domain.ReviewChangesRequested)
	_, err = f.mergeTool(t, "arch", "revised")
	wantRefusal(t, err, "MERGE_NOT_APPROVED", "asked for changes")
	if f.snapshot(t) != snap {
		t.Error("a refused merge changed the checkout")
	}

	// A review about another delegate approves nothing for this one.
	f.session(t, "approved", map[string]string{"approved.txt": "work\n"})
	f.review(t, "arch", "approved", domain.ReviewApproved)
	_, err = f.mergeTool(t, "arch", "delegate")
	wantRefusal(t, err, "MERGE_NOT_APPROVED")

	// Only the task's architect, and only on a session of its own task.
	f.session(t, "peer", nil)
	_, err = f.mergeTool(t, "peer", "delegate")
	wantRefusal(t, err, "ARCHITECT_ONLY")
	other, err := f.plan.CreateTicket(context.Background(), f.project.ID, "Another task", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	f.architect(t, "arch2", other)
	_, err = f.mergeTool(t, "arch2", "delegate")
	wantRefusal(t, err, "SESSION_NOT_ON_TASK")
	_, err = f.mergeTool(t, "arch", "nobody")
	wantRefusal(t, err, "SESSION_NOT_ON_TASK")
	if f.snapshot(t) != snap {
		t.Error("a refused merge changed the checkout")
	}

	// Approved, but the checkout is dirty / on another branch / conflicting.
	f.review(t, "arch", "delegate", domain.ReviewApproved)
	if err := os.WriteFile(filepath.Join(f.checkout, "README.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap = f.snapshot(t)
	_, err = f.mergeTool(t, "arch", "delegate")
	wantRefusal(t, err, "MERGE_CHECKOUT_DIRTY", "uncommitted changes to tracked files (README.md)")
	if f.snapshot(t) != snap {
		t.Error("a refusal changed the dirty checkout")
	}
	if last := f.sess.Get("delegate").LastMerge; last == nil || last.Code != domain.MergeCheckoutDirty {
		t.Errorf("the refusal is recorded on the session: %+v", last)
	}
	gitE2E(t, f.checkout, "checkout", "--", "README.md")

	gitE2E(t, f.checkout, "checkout", "-q", "-b", "elsewhere")
	snap = f.snapshot(t)
	_, err = f.mergeTool(t, "arch", "delegate")
	wantRefusal(t, err, "MERGE_WRONG_BRANCH", "is on elsewhere, not on the default branch main")
	if f.snapshot(t) != snap {
		t.Error("a refusal changed the checkout")
	}
	gitE2E(t, f.checkout, "checkout", "-q", "main")

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
	f.review(t, "arch", "conflict", domain.ReviewApproved)
	snap = f.snapshot(t)
	_, err = f.mergeTool(t, "arch", "conflict")
	wantRefusal(t, err, "MERGE_CONFLICT", "conflicts in shared.txt")
	if f.snapshot(t) != snap {
		t.Error("a conflict changed the checkout")
	}

	// Approved, clean: it merges, by branch name too, and answers the new HEAD.
	before := gitE2E(t, f.checkout, "rev-parse", "HEAD")
	out, err := f.mergeTool(t, "arch", "agent/delegate")
	if err != nil {
		t.Fatal(err)
	}
	head := gitE2E(t, f.checkout, "rev-parse", "HEAD")
	if out["merged"] != true || out["head"] != head || out["into"] != "main" || head == before {
		t.Fatalf("answer = %v (HEAD %s, was %s)", out, head, before)
	}
	if parents := strings.Fields(gitE2E(t, f.checkout, "rev-list", "--parents", "-n1", "HEAD")); len(parents) != 3 {
		t.Errorf("not a --no-ff merge commit: %v", parents)
	}

	// The record on the session is the answer, and the one the web action writes.
	last := f.sess.Get("delegate").LastMerge
	if last == nil || !last.Merged || last.Head != head {
		t.Fatalf("recorded = %+v", last)
	}
	var recorded map[string]any
	raw, _ := json.Marshal(last)
	_ = json.Unmarshal(raw, &recorded)
	delete(recorded, "at") // the store keeps milliseconds
	delete(out, "at")
	if !reflect.DeepEqual(recorded, out) {
		t.Errorf("answer and record differ:\n%v\n%v", out, recorded)
	}
	f.session(t, "web", map[string]string{"web.txt": "web\n"})
	webResult, err := f.merge.MergeSessionBranch(context.Background(), "web")
	if err != nil {
		t.Fatal(err)
	}
	webRecord := f.sess.Get("web").LastMerge
	if webRecord == nil || webRecord.Merged != last.Merged || webRecord.Into != last.Into || webRecord.Head != webResult.Head {
		t.Errorf("the web action records %+v, the tool %+v", webRecord, last)
	}

	if got := f.remoteRefs(t); got != remote {
		t.Errorf("origin changed:\nbefore %s\nafter  %s", remote, got)
	}
}
