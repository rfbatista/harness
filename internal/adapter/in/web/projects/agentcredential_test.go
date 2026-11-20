package projects

import (
	"errors"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

type fakeProjectCredentials struct {
	own ports.AgentCredentialStatus
	err error
}

func (f fakeProjectCredentials) StatusFor(string) (ports.AgentCredentialStatus, error) {
	return f.own, f.err
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
	v, err := NewAgentCredentialView(nil, nil, "p1", "alpha")
	if v != nil || err != nil {
		t.Fatalf("view %v, err %v", v, err)
	}
	if _, err := NewAgentCredentialView(fakeProjectCredentials{err: errors.New("locked")}, nil, "p1", "alpha"); err == nil {
		t.Fatal("a read failure is swallowed here; the handler decides to leave the section out")
	}
}

func TestAProjectWithoutItsOwnCredentialShowsGlobalAndWhatItFallsBackTo(t *testing.T) {
	v, _ := NewAgentCredentialView(fakeProjectCredentials{}, fakeGlobalCredential{ports.AgentCredentialStatus{Set: true, Kind: ports.AgentCredentialOAuth}}, "p1", "alpha")
	if v.ChipText() != "Global" || v.ChipTone() != "" || v.PasteLabel() != "Paste a token or key" || v.PasteTone() != "primary" {
		t.Fatalf("chip %q/%q, paste %q/%q", v.ChipText(), v.ChipTone(), v.PasteLabel(), v.PasteTone())
	}
	if !strings.Contains(v.FallbackLine(), "global Agent credential") {
		t.Fatalf("line %q", v.FallbackLine())
	}
	v, _ = NewAgentCredentialView(fakeProjectCredentials{}, nil, "p1", "alpha")
	if !strings.Contains(v.FallbackLine(), "No global Agent credential") {
		t.Fatalf("line %q", v.FallbackLine())
	}
}

func TestAProjectsOwnCredentialShowsItsKindAndDayNeverAValue(t *testing.T) {
	saved := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	v, _ := NewAgentCredentialView(fakeProjectCredentials{own: ports.AgentCredentialStatus{Set: true, Kind: ports.AgentCredentialAPIKey, Updated: saved}}, nil, "p1", "alpha")
	if v.ChipText() != "Own credential" || v.ChipTone() != "signal" || v.KindName() != "Anthropic API key" || v.SavedDate() != "2026-11-20" || v.PasteLabel() != "Replace with a pasted token or key" {
		t.Fatalf("view %+v", v)
	}
}
