package critic

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeSessions struct{}

func (fakeSessions) Find(_ context.Context, id string) (*domain.Session, error) {
	return &domain.Session{ID: id, ProjectID: "p", RepositoryID: "r", TicketID: "t", WorkspaceID: "w", WorkingDir: "/w"}, nil
}

type fakeGate struct{ notGated bool }

func (g fakeGate) Gated(ctx context.Context, id string) (*domain.Session, domain.GuardConfig, error) {
	if g.notGated {
		return nil, domain.GuardConfig{}, errors.New("not gated")
	}
	s, _ := fakeSessions{}.Find(ctx, id)
	return s, domain.GuardConfig{}, nil
}

type fakeSettings struct {
	ports.AgentSystemSettings
	off   bool
	model string
}

func (f fakeSettings) Effective(context.Context, string, string) (domain.EffectiveAgentSystem, error) {
	eff := domain.ResolveAgentSystem(domain.AgentSystemHarnessDefaults{})
	eff.Review.Critic.Enabled.Value = !f.off
	eff.Review.Critic.Model.Value = f.model
	return eff, nil
}

type fakeReviews struct {
	mu   sync.Mutex
	seen map[string]string
}

func (r *fakeReviews) key(s, b, h string) string { return s + "|" + b + "|" + h }
func (r *fakeReviews) Seen(_ context.Context, s, b, h string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.seen[r.key(s, b, h)]
	return ok, nil
}
func (r *fakeReviews) Claim(_ context.Context, s, b, h, run string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[string]string{}
	}
	if _, ok := r.seen[r.key(s, b, h)]; ok {
		return false, nil
	}
	r.seen[r.key(s, b, h)] = run
	return true, nil
}
func (r *fakeReviews) Release(_ context.Context, s, b, h string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.seen, r.key(s, b, h))
	return nil
}

type fakeDiff struct{ head string }

func (d *fakeDiff) Range(context.Context, *domain.Session) (string, string, error) {
	return "base0000", d.head, nil
}
func (d *fakeDiff) Diff(context.Context, *domain.Session, string) (string, error) {
	return "+added line", nil
}
func (d *fakeDiff) File(_ context.Context, _ *domain.Session, p string) string {
	if p == "AGENTS.md" {
		return "be kind to the reader"
	}
	return ""
}

type fakeStarter struct {
	reqs []ports.InteractiveRequest
	err  error
}

func (s *fakeStarter) StartInteractive(_ context.Context, r ports.InteractiveRequest) (*domain.Session, ports.AgentSpec, error) {
	if s.err != nil {
		return nil, ports.AgentSpec{}, s.err
	}
	s.reqs = append(s.reqs, r)
	return &domain.Session{ID: "critic"}, ports.AgentSpec{}, nil
}

type fakeDocs struct{}

func (fakeDocs) ListTicketDocuments(context.Context, string) []*domain.Document {
	return []*domain.Document{
		{Title: "Spec: harness — thing", Content: "the spec text"},
		{Title: "Notes", Content: "not for the critic"},
	}
}

func newService(settings fakeSettings, diff *fakeDiff, st *fakeStarter, gate fakeGate) *Service {
	return New(Deps{Sessions: fakeSessions{}, Settings: settings, Gated: gate, Reviews: &fakeReviews{}, Diff: diff, Documents: fakeDocs{}, Starter: st})
}

func finished(trigger domain.GateTrigger, result domain.GateRunResult) ports.GateRunChange {
	return ports.GateRunChange{SessionID: "author", RunID: "run1", Trigger: trigger, Phase: "finished", Result: result}
}

func TestTheCriticLaunchesOncePerDiffAndAgainOnANewHead(t *testing.T) {
	st, diff := &fakeStarter{}, &fakeDiff{head: "head1111"}
	svc := newService(fakeSettings{model: "opus"}, diff, st, fakeGate{})
	ctx := context.Background()

	for i, want := range []int{1, 1} { // a green run, then the same diff green again
		if _, err := svc.Handle(ctx, finished(domain.GateTriggerDeclaration, domain.GateRunGreen)); err != nil {
			t.Fatal(err)
		}
		if len(st.reqs) != want {
			t.Fatalf("after run %d: %d critics, want %d", i+1, len(st.reqs), want)
		}
	}
	diff.head = "head2222"
	if _, err := svc.Handle(ctx, finished(domain.GateTriggerPerson, domain.GateRunGreen)); err != nil || len(st.reqs) != 2 {
		t.Fatalf("a new head: %d critics (err %v), want 2", len(st.reqs), err)
	}
}

func TestTheCriticStartsReadOnlyOnTheCriticModelWithAFreshContext(t *testing.T) {
	st := &fakeStarter{}
	svc := newService(fakeSettings{model: "opus"}, &fakeDiff{head: "head1111"}, st, fakeGate{})
	if _, err := svc.Handle(context.Background(), finished(domain.GateTriggerDeclaration, domain.GateRunGreen)); err != nil {
		t.Fatal(err)
	}
	r := st.reqs[0]
	if r.Mode != "review" || r.Access != "read_only" || r.WorktreeID != "w" || r.Model != "opus" || r.TicketID != "t" {
		t.Errorf("request = %+v", r)
	}
	if r.Critic == nil || r.Critic.AuthorSessionID != "author" || r.Critic.GateRunID != "run1" {
		t.Errorf("critic link = %+v", r.Critic)
	}
	for _, want := range []string{"+added line", "the spec text", "be kind to the reader", "Critic findings"} {
		if !strings.Contains(r.Prompt, want) {
			t.Errorf("the starting context lacks %q", want)
		}
	}
	if strings.Contains(r.Prompt, "not for the critic") {
		t.Error("a document that is not a Spec or Contract reached the critic")
	}
}

func TestNoCriticForAPracticeRunARedRunACriticOffOrAnUngatedSession(t *testing.T) {
	ctx := context.Background()
	for name, run := range map[string]struct {
		svc *Service
		in  ports.GateRunChange
	}{
		"practice": {newService(fakeSettings{}, &fakeDiff{head: "h"}, &fakeStarter{}, fakeGate{}), finished(domain.GateTriggerPractice, domain.GateRunGreen)},
		"red":      {newService(fakeSettings{}, &fakeDiff{head: "h"}, &fakeStarter{}, fakeGate{}), finished(domain.GateTriggerDeclaration, domain.GateRunRed)},
		"off":      {newService(fakeSettings{off: true}, &fakeDiff{head: "h"}, &fakeStarter{}, fakeGate{}), finished(domain.GateTriggerDeclaration, domain.GateRunGreen)},
		"ungated":  {newService(fakeSettings{}, &fakeDiff{head: "h"}, &fakeStarter{}, fakeGate{notGated: true}), finished(domain.GateTriggerDeclaration, domain.GateRunGreen)},
		"started":  {newService(fakeSettings{}, &fakeDiff{head: "h"}, &fakeStarter{}, fakeGate{}), ports.GateRunChange{SessionID: "author", Trigger: domain.GateTriggerDeclaration, Phase: "started"}},
	} {
		got, err := run.svc.Handle(ctx, run.in)
		if got != nil || err != nil {
			t.Errorf("%s: launched %v (err %v)", name, got, err)
		}
	}
}

func TestADiffWhoseCriticFailedToStartIsReviewedByALaterRun(t *testing.T) {
	st := &fakeStarter{err: errors.New("no sandbox")}
	svc := newService(fakeSettings{}, &fakeDiff{head: "h"}, st, fakeGate{})
	if _, err := svc.Handle(context.Background(), finished(domain.GateTriggerDeclaration, domain.GateRunGreen)); err == nil {
		t.Fatal("the failure was swallowed")
	}
	st.err = nil
	if got, err := svc.Handle(context.Background(), finished(domain.GateTriggerDeclaration, domain.GateRunGreen)); got == nil || err != nil {
		t.Fatalf("the retry launched %v (err %v)", got, err)
	}
}
