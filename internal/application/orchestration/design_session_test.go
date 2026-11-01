package orchestration

import (
	"context"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// brandOne is the project p1's design system "Brand One", ds1.
type brandOne struct{}

func (brandOne) GetDesignSystem(_ context.Context, id string) (*domain.DesignSystem, error) {
	if id != "ds1" {
		return nil, &domain.StructuredError{Code: "DESIGN_SYSTEM_NOT_FOUND", Message: "design system not found"}
	}
	return &domain.DesignSystem{ID: id, ProjectID: "p1", Name: "Brand One", Slug: "brand-one"}, nil
}

// The design-session brief is pinned byte for byte, like the project brief: a
// change to it is deliberate. UPDATE_GOLDEN=1 rewrites the file.
func TestDesignSessionBrief_IsPinned(t *testing.T) {
	got := designSessionBrief(designSessionBriefInput{
		Project:      &domain.Project{ID: "p1", Name: "harness"},
		DesignSystem: "Brand One",
		Repositories: []*domain.Repository{{ID: "r1", Name: "api", Description: "the server"}},
	})
	golden(t, "design_session_brief", got)
}

// A design session starts on its project with no task, bound to one design
// system, with its own brief, an opening message that carries the person's
// instruction, and repositories it can read but not write.
func TestStartInteractive_DesignSessionIsBoundToItsDesignSystemWithNoTask(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.catalog.DesignSystems = brandOne{}
	sess, launch := startInteractive(t, svc, InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", Mode: "designsystem", DesignSystemID: "ds1", Prompt: "Set up tokens for a calm look.",
	})

	if sess.TicketID != "" || sess.Mode != domain.SessionModeDesignSystem || sess.DesignSystemID != "ds1" || !sess.Interactive {
		t.Fatalf("session = %+v", sess)
	}
	if got := sessionOf(svc, sess.ID); got == nil || got.DesignSystemID != "ds1" || got.Mode != domain.SessionModeDesignSystem {
		t.Fatalf("stored session = %+v", got)
	}
	p := launch.Spec.Prompt
	if !strings.Contains(p, `design system "Brand One"`) || !strings.HasSuffix(p, "Set up tokens for a calm look.") {
		t.Errorf("first message = %q", p)
	}
	if strings.Contains(p, "task-product") {
		t.Errorf("a design session is not a product session: %q", p)
	}
	sys := launch.Spec.AppendSystem
	if !strings.Contains(sys, "## Your design session") || strings.Contains(sys, "product session") {
		t.Errorf("system prompt:\n%s", sys)
	}
	if strings.Contains(sys, "This repository uses the project design system") {
		t.Errorf("a design session has no repository pointer:\n%s", sys)
	}
}

func TestStartInteractive_ADesignSessionNeedsItsDesignSystemOfTheProject(t *testing.T) {
	svc, prov := newInteractiveService(t)
	svc.catalog.DesignSystems = brandOne{}
	for name, req := range map[string]InteractiveRequest{
		"none":    {ProjectID: "p1", RepositoryID: "r1", Mode: "designsystem"},
		"unknown": {ProjectID: "p1", RepositoryID: "r1", Mode: "designsystem", DesignSystemID: "nope"},
	} {
		_, _, err := svc.StartInteractive(context.Background(), req)
		want := map[string]string{"none": "INVALID_INPUT", "unknown": "DESIGN_SYSTEM_NOT_FOUND"}[name]
		if codeOf(err) != want {
			t.Errorf("%s: err = %v, want %s", name, err, want)
		}
	}
	if len(prov.created) != 0 {
		t.Fatalf("a refused start provisioned a worktree: %v", prov.created)
	}
}

// Every other mode refuses a design system id: only a design session is bound.
func TestStartInteractive_OnlyADesignSessionTakesADesignSystem(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", DesignSystemID: "ds1"})
	if codeOf(err) != "INVALID_INPUT" {
		t.Fatalf("err = %v, want INVALID_INPUT", err)
	}
}

// A design session changes no repository through any route: its worktree, the
// git directory and every repository are read-only in its sandbox.
func TestDesignSession_ReadsRepositoriesAndWritesNone(t *testing.T) {
	policy := BuildSandboxPolicy(SandboxSession{
		Mode: domain.SessionModeDesignSystem, Worktree: "/ws/own", GitCommonDir: "/repo/.git",
		Repositories: []string{"/repo", "/other"}, ReadAllRepos: false, VirtualShell: "/shell", TempDir: "/tmp/s",
	})
	for _, path := range []string{"/ws/own", "/repo", "/other"} {
		if slices.Contains(policy.Writable, path) {
			t.Errorf("%s is writable", path)
		}
		if !slices.Contains(policy.Readable, path) {
			t.Errorf("%s is not readable", path)
		}
	}
	for _, w := range policy.Writable {
		if strings.HasPrefix(w, "/repo") || strings.HasPrefix(w, "/ws") {
			t.Errorf("writable %s is inside a repository", w)
		}
	}
	if !slices.Contains(policy.Writable, "/shell") {
		t.Errorf("the virtual shell is not writable: %v", policy.Writable)
	}
	if got := domain.SandboxVisibilityFor(domain.SessionModeDesignSystem); got != domain.SandboxAllReposReadOnly {
		t.Errorf("visibility = %q", got)
	}
}
