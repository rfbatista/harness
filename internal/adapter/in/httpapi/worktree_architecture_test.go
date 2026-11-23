package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeAttribution struct {
	ports.Orchestration
	session  string
	worktree *ports.CheckedWorktree
	err      error
	asked    string
}

func (f *fakeAttribution) CommitAttribution(_ context.Context, ws string) (ports.CommitAttribution, error) {
	f.asked = ws
	out := ports.CommitAttribution{SessionID: f.session}
	if f.worktree != nil {
		out.RepositoryID, out.ProjectID, out.Dir = f.worktree.RepositoryID, f.worktree.ProjectID, f.worktree.Dir
	}
	return out, f.err
}

type recordingRules struct {
	checks  []ports.ArchitectureCheck
	verdict ports.ArchitectureVerdict
}

func (r *recordingRules) CheckArchitecture(_ context.Context, c ports.ArchitectureCheck) ports.ArchitectureVerdict {
	r.checks = append(r.checks, c)
	return r.verdict
}

func postCommit(t *testing.T, h *Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	return rec
}

func TestAWorktreesCommitEndpointChecksTheCommitAsTheSessionItAttributesItTo(t *testing.T) {
	who := &fakeAttribution{session: "sess-a"}
	rules := &recordingRules{}
	h := NewHandler(Services{Sessions: who, ArchitectureRules: rules})

	rec := postCommit(t, h, WorktreeArchitecturePath("ws1")+"/commit", "a.go\x00b.go\x00")
	if rec.Code != http.StatusOK || who.asked != "ws1" {
		t.Fatalf("%d asked %q: %s", rec.Code, who.asked, rec.Body)
	}
	if len(rules.checks) != 1 || rules.checks[0].SessionID != "sess-a" || rules.checks[0].Point != domain.PointBeforeCommit ||
		strings.Join(rules.checks[0].Paths, ",") != "a.go,b.go" {
		t.Errorf("checks = %+v", rules.checks)
	}
	if got := rec.Header().Get("X-Harness-Attributed-To"); got != "sess-a" {
		t.Errorf("attributed to %q", got)
	}
}

func TestAWorktreesCommitEndpointFailsTheCommitOnABlockingRuleAndRefusesAWorktreeWithNoSession(t *testing.T) {
	who := &fakeAttribution{session: "sess-a"}
	rules := &recordingRules{verdict: ports.ArchitectureVerdict{Deny: "layer rule"}}
	h := NewHandler(Services{Sessions: who, ArchitectureRules: rules})
	if rec := postCommit(t, h, WorktreeArchitecturePath("ws1")+"/commit", "a.go"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "layer rule") {
		t.Errorf("blocking rule: %d %s", rec.Code, rec.Body)
	}

	// A worktree no session is on is still checked, as itself.
	who.session, who.worktree = "", &ports.CheckedWorktree{RepositoryID: "r1", ProjectID: "p1", Dir: "/w"}
	rules.verdict, rules.checks = ports.ArchitectureVerdict{}, nil
	if rec := postCommit(t, h, WorktreeArchitecturePath("ws1")+"/commit", "a.go"); rec.Code != http.StatusOK ||
		len(rules.checks) != 1 || rules.checks[0].SessionID != "" || rules.checks[0].Worktree == nil || rules.checks[0].Worktree.Dir != "/w" {
		t.Errorf("no session: %d %s, checks %+v; want one check carrying the worktree", rec.Code, rec.Body, rules.checks)
	}

	// An unknown worktree is the one thing that is refused.
	who.err = &domain.StructuredError{Code: "WORKTREE_NOT_FOUND", Message: "no such worktree"}
	rules.checks = nil
	if rec := postCommit(t, h, WorktreeArchitecturePath("ws1")+"/commit", "a.go"); rec.Code != http.StatusNotFound || len(rules.checks) != 0 {
		t.Errorf("unknown worktree: %d %s, checks %v", rec.Code, rec.Body, rules.checks)
	}
}

func TestAWorktreesCommitEndpointAnswersOnlyTheLoopbackAndNamesNothingToOthers(t *testing.T) {
	who := &fakeAttribution{session: "sess-a"}
	h := NewHandler(Services{Sessions: who, ArchitectureRules: &recordingRules{}})
	req := httptest.NewRequest(http.MethodPost, WorktreeArchitecturePath("ws1")+"/commit", strings.NewReader("a.go"))
	req.RemoteAddr = "203.0.113.9:4000"
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || who.asked != "" || rec.Header().Get("X-Harness-Attributed-To") != "" {
		t.Errorf("%d, asked %q, header %q", rec.Code, who.asked, rec.Header().Get("X-Harness-Attributed-To"))
	}
}
