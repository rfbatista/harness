package orchestration

import (
	"context"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeAssignments is the repository agent lists: which agents are unassigned
// in which repository.
type fakeAssignments struct{ unassigned map[string]map[string]bool }

func (f *fakeAssignments) unassign(repo, agent string) {
	if f.unassigned == nil {
		f.unassigned = map[string]map[string]bool{}
	}
	if f.unassigned[repo] == nil {
		f.unassigned[repo] = map[string]bool{}
	}
	f.unassigned[repo][agent] = true
}

func (f *fakeAssignments) assign(repo, agent string) { delete(f.unassigned[repo], agent) }

func (f *fakeAssignments) IsUnassigned(_ context.Context, repo, agent string) bool {
	return f.unassigned[repo][agent]
}

func (f *fakeAssignments) CheckAssigned(_ context.Context, repo, agent string) error {
	if f.unassigned[repo][agent] {
		return &domain.StructuredError{Code: "AGENT_NOT_ASSIGNED", Message: "AGENT_NOT_ASSIGNED: " + agent + " in " + repo}
	}
	return nil
}

var _ ports.AgentAssignments = (*fakeAssignments)(nil)

func assignedWorld(t *testing.T) (*Service, *fakeResolver, *fakeAssignments, *fakeProvisioner) {
	t.Helper()
	svc, res := catalogWorld(t)
	as := &fakeAssignments{}
	svc.UseAssignments(as)
	return svc, res, as, svc.workspaces.(*fakeProvisioner)
}

func TestSessionStart_RefusesAnAgentUnassignedFromTheRepositoryBeforeProvisioning(t *testing.T) {
	svc, _, as, prov := assignedWorld(t)
	as.unassign("r1", "backend")

	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if codeOf(err) != "AGENT_NOT_ASSIGNED" {
		t.Fatalf("interactive err = %v", err)
	}
	_, err = svc.Start(t.Context(), StartRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Task: "x", AgentID: "a1"})
	if codeOf(err) != "AGENT_NOT_ASSIGNED" {
		t.Fatalf("headless err = %v", err)
	}
	if len(prov.created) != 0 {
		t.Fatalf("a refused start provisioned a worktree: %v", prov.created)
	}

	// Assigned again, and plain claude, start.
	as.assign("r1", "backend")
	if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"}); err != nil {
		t.Fatalf("assigned agent: %v", err)
	}
	as.unassign("r1", "backend")
	if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1"}); err != nil {
		t.Fatalf("no agent: %v", err)
	}
}

func TestSessionStart_NotEnabledIsRefusedBeforeNotAssigned(t *testing.T) {
	svc, res, as, _ := assignedWorld(t)
	res.proj.EnabledAgentsMode = domain.AgentsSelected
	res.proj.EnabledAgents = []string{"someone-else"}
	as.unassign("r1", "backend")

	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if codeOf(err) != "AGENT_NOT_ENABLED" {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionResume_IsRefusedWhileItsAgentIsUnassignedAndTheReadSaysSo(t *testing.T) {
	svc, _, as, _ := assignedWorld(t)
	ctx := t.Context()
	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}

	as.unassign("r1", "backend")
	_, _, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if codeOf(err) != "AGENT_NOT_ASSIGNED" {
		t.Fatalf("resume err = %v", err)
	}
	got, err := svc.Get(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AgentUnassigned || got.Resumable || got.ResumeBlocked != "AGENT_NOT_ASSIGNED" {
		t.Fatalf("read = unassigned %v resumable %v blocked %q", got.AgentUnassigned, got.Resumable, got.ResumeBlocked)
	}
	list, err := svc.List(ctx, ports.SessionFilter{ProjectID: "p1"})
	if err != nil || len(list) == 0 || !list[0].AgentUnassigned || list[0].ResumeBlocked != "AGENT_NOT_ASSIGNED" {
		t.Fatalf("list = %+v, err %v", list, err)
	}

	as.assign("r1", "backend")
	got, _ = svc.Get(ctx, sess.ID)
	if got.AgentUnassigned || !got.Resumable || got.ResumeBlocked != "" {
		t.Fatalf("after assigning again: unassigned %v resumable %v blocked %q", got.AgentUnassigned, got.Resumable, got.ResumeBlocked)
	}
	if _, _, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatalf("resume after assigning again: %v", err)
	}
}

func TestUnassigningLeavesARunningSessionAlone(t *testing.T) {
	svc, _, as, _ := assignedWorld(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	as.unassign("r1", "backend")

	got, err := svc.Get(t.Context(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.IsTerminal() || got.ResumeBlocked != "SESSION_ALREADY_RUNNING" {
		t.Fatalf("running session = %v / %q", got.Status, got.ResumeBlocked)
	}
}
