package projects

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

type fakeProjectCredentials struct {
	own       ports.AgentCredentialStatus
	effective ports.EffectiveCredential
	err       error
	// deadline, when set, takes the deadline DetailFor's context carries.
	deadline *time.Time
}

func (f fakeProjectCredentials) StatusFor(string) (ports.AgentCredentialStatus, error) {
	return f.own, f.err
}
func (f fakeProjectCredentials) DetailFor(ctx context.Context, _ string) (ports.ProjectCredentialStatus, error) {
	if f.deadline != nil {
		*f.deadline, _ = ctx.Deadline()
	}
	return ports.ProjectCredentialStatus{AgentCredentialStatus: f.own, Effective: f.effective}, f.err
}
func (fakeProjectCredentials) SetFor(string, string, string) (ports.AgentCredentialStatus, error) {
	return ports.AgentCredentialStatus{}, nil
}
func (fakeProjectCredentials) RemoveFor(string) (ports.AgentCredentialStatus, error) {
	return ports.AgentCredentialStatus{}, nil
}

type fakeGlobalCredential struct{ st ports.AgentCredentialStatus }

func (f fakeGlobalCredential) Status() ports.AgentCredentialStatus { return f.st }
func (fakeGlobalCredential) Set(string, string) (ports.AgentCredentialStatus, error) {
	return ports.AgentCredentialStatus{}, nil
}
func (fakeGlobalCredential) Remove() (ports.AgentCredentialStatus, error) {
	return ports.AgentCredentialStatus{}, nil
}

func TestTheAgentCredentialSectionIsLeftOutWithoutTheService(t *testing.T) {
	v, err := NewAgentCredentialView(context.Background(), nil, nil, "p1", "alpha")
	if v != nil || err != nil {
		t.Fatalf("view %v, err %v", v, err)
	}
	if _, err := NewAgentCredentialView(context.Background(), fakeProjectCredentials{err: errors.New("locked")}, nil, "p1", "alpha"); err == nil {
		t.Fatal("a read failure is swallowed here; the handler decides to leave the section out")
	}
}

func TestAProjectWithoutItsOwnCredentialShowsGlobalAndWhatItFallsBackTo(t *testing.T) {
	for effective, want := range map[ports.EffectiveCredential]string{
		{Source: ports.EffectiveGlobalPasted, Kind: ports.AgentCredentialOAuth}:                                                 "Uses the global pasted OAuth token",
		{Source: ports.EffectiveGlobalPasted, Kind: ports.AgentCredentialAPIKey}:                                                "Uses the global pasted API key",
		{Source: ports.EffectiveGlobalLogin, Kind: ports.AgentCredentialLogin, Account: "ana@example.com", Subscription: "max"}: "Uses the global login: ana@example.com, max",
		{Source: ports.EffectiveNone}: "No credential: agents cannot start",
	} {
		v, _ := NewAgentCredentialView(context.Background(), fakeProjectCredentials{effective: effective}, nil, "p1", "alpha")
		if v.ChipText() != "Global" || v.ChipTone() != "" || v.PasteLabel() != "Paste a token or key" || v.PasteTone() != "primary" {
			t.Fatalf("chip %q/%q, paste %q/%q", v.ChipText(), v.ChipTone(), v.PasteLabel(), v.PasteTone())
		}
		if !strings.Contains(v.FallbackLine(), want) {
			t.Fatalf("line %q, want %q", v.FallbackLine(), want)
		}
	}
}

func TestAProjectsOwnCredentialShowsItsKindAndDayNeverAValue(t *testing.T) {
	saved := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	v, _ := NewAgentCredentialView(context.Background(), fakeProjectCredentials{own: ports.AgentCredentialStatus{Set: true, Kind: ports.AgentCredentialAPIKey, Updated: saved}}, nil, "p1", "alpha")
	if v.ChipText() != "Own credential" || v.ChipTone() != "signal" || v.KindName() != "Anthropic API key" || v.SavedDate() != "2026-11-20" || v.PasteLabel() != "Replace with a pasted token or key" {
		t.Fatalf("view %+v", v)
	}
}

func TestThePageWaitsOnlyAShortWhileForTheLoginStatus(t *testing.T) {
	var deadline time.Time
	before := time.Now()
	if _, err := NewAgentCredentialView(context.Background(), fakeProjectCredentials{deadline: &deadline}, nil, "p1", "alpha"); err != nil {
		t.Fatal(err)
	}
	if deadline.IsZero() || deadline.After(before.Add(StatusTimeout+time.Second)) {
		t.Errorf("the status read's deadline is %v, want within %v of the start", deadline, StatusTimeout)
	}
}
