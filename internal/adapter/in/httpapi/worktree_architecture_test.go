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
	session string
	err     error
	asked   string
}

func (f *fakeAttribution) CommitAttribution(_ context.Context, ws string) (string, error) {
	f.asked = ws
	return f.session, f.err
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

	who.err = &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no session with write access is on this worktree"}
	rules.checks = nil
	if rec := postCommit(t, h, WorktreeArchitecturePath("ws1")+"/commit", "a.go"); rec.Code != http.StatusNotFound || len(rules.checks) != 0 {
		t.Errorf("no session: %d %s, checks %v", rec.Code, rec.Body, rules.checks)
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
