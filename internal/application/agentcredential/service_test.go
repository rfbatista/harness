package agentcredential

import (
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

type memStore struct{ rec *ports.AgentCredentialRecord }

func (m *memStore) Get() (*ports.AgentCredentialRecord, error) { return m.rec, nil }
func (m *memStore) Put(r ports.AgentCredentialRecord) error    { m.rec = &r; return nil }
func (m *memStore) Delete() error                              { m.rec = nil; return nil }

// reverseBox is a stand-in cipher: enough to tell sealed from plain.
type reverseBox struct{}

func (reverseBox) KeyID() string { return "k1" }
func (reverseBox) Seal(p []byte, _ bool) ([]byte, string, error) {
	out := make([]byte, len(p))
	for i, b := range p {
		out[len(p)-1-i] = b ^ 0x55
	}
	return out, "k1", nil
}
func (b reverseBox) Open(s []byte) ([]byte, error) {
	out := make([]byte, len(s))
	for i, c := range s {
		out[len(s)-1-i] = c ^ 0x55
	}
	return out, nil
}

func newService() (*Service, *memStore) {
	st := &memStore{}
	return New(st, reverseBox{}, func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }), st
}

func TestAnOAuthTokenIsExposedAsTheOAuthVariable(t *testing.T) {
	svc, _ := newService()
	if _, err := svc.Set(ports.AgentCredentialOAuth, " sk-ant-oat01-abc \n"); err != nil {
		t.Fatal(err)
	}
	if got := svc.Env(); len(got) != 1 || got[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-abc" {
		t.Fatalf("env %v", got)
	}
}

func TestAnAPIKeyIsExposedAsTheAPIKeyVariable(t *testing.T) {
	svc, _ := newService()
	if _, err := svc.Set(ports.AgentCredentialAPIKey, "sk-ant-api03-xyz"); err != nil {
		t.Fatal(err)
	}
	if got := svc.Env(); len(got) != 1 || got[0] != "ANTHROPIC_API_KEY=sk-ant-api03-xyz" {
		t.Fatalf("env %v", got)
	}
}

func TestReplacingAndRemovingTheCredential(t *testing.T) {
	svc, _ := newService()
	if svc.Status().Set || svc.Env() != nil {
		t.Fatalf("a fresh service has a credential: %+v", svc.Status())
	}
	_, _ = svc.Set(ports.AgentCredentialOAuth, "sk-ant-oat01-one")
	st, err := svc.Set(ports.AgentCredentialAPIKey, "sk-ant-api03-two")
	if err != nil || !st.Set || st.Kind != ports.AgentCredentialAPIKey {
		t.Fatalf("replace = %+v, %v", st, err)
	}
	if got := svc.Env(); len(got) != 1 || !strings.HasPrefix(got[0], "ANTHROPIC_API_KEY=") {
		t.Fatalf("env after replace %v", got)
	}
	st, err = svc.Remove()
	if err != nil || st.Set || svc.Env() != nil {
		t.Fatalf("remove = %+v, %v, env %v", st, err, svc.Env())
	}
}

func TestTheStoredValueIsSealedAndTheStatusDoesNotCarryIt(t *testing.T) {
	svc, st := newService()
	status, _ := svc.Set(ports.AgentCredentialOAuth, "sk-ant-oat01-topsecret")
	if strings.Contains(string(st.rec.Sealed), "topsecret") {
		t.Fatal("the stored value is plaintext")
	}
	if strings.Contains(fmtStatus(status), "topsecret") {
		t.Fatal("the status carries the value")
	}
}

func TestAnEmptyValueOrUnknownKindIsRefused(t *testing.T) {
	svc, _ := newService()
	if _, err := svc.Set(ports.AgentCredentialOAuth, "  "); err == nil {
		t.Error("empty value accepted")
	}
	if _, err := svc.Set("password", "x"); err == nil {
		t.Error("unknown kind accepted")
	}
}

func fmtStatus(s ports.AgentCredentialStatus) string { return s.Kind + s.Updated.String() }
