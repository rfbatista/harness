//go:build !windows

package app

import (
	"context"
	"sync"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Guard — the configuration a session keeps (ADR 0011, 0012): taken once when
// the session starts or resumes, never again while it runs.

// fakeAgentSystem answers the effective configuration a test sets.
type fakeAgentSystem struct {
	mu       sync.Mutex
	retry    int
	err      error
	requests int
}

func (f *fakeAgentSystem) set(retry int) { f.mu.Lock(); f.retry = retry; f.mu.Unlock() }

func (f *fakeAgentSystem) Effective(_ context.Context, _, _ string) (domain.EffectiveAgentSystem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if f.err != nil {
		return domain.EffectiveAgentSystem{}, f.err
	}
	eff := domain.ResolveAgentSystem(domain.AgentSystemHarnessDefaults{})
	eff.Guard.RetryLimit = domain.AgentSystemField[int]{Value: f.retry, Source: domain.SourceProject}
	return eff, nil
}

func TestASessionKeepsTheGuardConfigurationItStartedWithAndAResumeTakesTheCurrentOne(t *testing.T) {
	e := newShellE2E(t)
	settings := &fakeAgentSystem{retry: 3}
	e.orch.AgentSystem = settings
	sess, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Guarded"), Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	kept, ok := e.orch.GuardConfig(e.ctx, sess.ID)
	if !ok || kept.RetryLimit != 3 || kept.Mode != domain.SessionModeImplement || !kept.Gated(false) {
		t.Fatalf("kept at start = %+v (ok %v)", kept, ok)
	}

	settings.set(5) // a save while the session runs
	if kept, _ := e.orch.GuardConfig(e.ctx, sess.ID); kept.RetryLimit != 3 {
		t.Errorf("a running session's retry limit = %d after a save, want 3", kept.RetryLimit)
	}
	if settings.requests != 1 {
		t.Errorf("the settings were read %d times, want once at start", settings.requests)
	}

	e.orch.Transcripts = transcriptsThatResume{}
	if _, err := e.orch.EndInteractive(e.ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.orch.ResumeInteractive(e.ctx, ports.ResumeRequest{SessionID: sess.ID, RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatal(err)
	}
	if kept, _ := e.orch.GuardConfig(e.ctx, sess.ID); kept.RetryLimit != 5 {
		t.Errorf("a resumed session's retry limit = %d, want 5", kept.RetryLimit)
	}
}

func TestASessionTheSettingsCannotResolveStartsAsBeforeAndIsNotGated(t *testing.T) {
	e := newShellE2E(t)
	e.orch.AgentSystem = &fakeAgentSystem{err: &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}}
	sess, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Unresolved"), Mode: "implement"})
	if err != nil {
		t.Fatalf("the start must go on: %v", err)
	}
	kept, ok := e.orch.GuardConfig(e.ctx, sess.ID)
	if !ok || kept.Unresolved == "" || kept.Gated(true) {
		t.Errorf("kept = %+v (ok %v), want an unresolved configuration that is not gated", kept, ok)
	}
}
