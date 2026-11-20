package agentcredential

import (
	"context"
	"errors"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeSignIn is a project's sign-in: a login that a logout removes, unless it
// is told to leave it (claude exits 0 and the login stays).
type fakeSignIn struct {
	login     *bool
	keepLogin bool
	logoutErr error
	ports.AgentOAuth
}

func (f fakeSignIn) Status(context.Context) (ports.AgentAuthStatus, error) {
	return ports.AgentAuthStatus{LoggedIn: *f.login, Email: "ana@example.com", SubscriptionType: "max"}, nil
}
func (f fakeSignIn) Logout(context.Context) error {
	if f.logoutErr == nil && !f.keepLogin {
		*f.login = false
	}
	return f.logoutErr
}

type fakeLogins struct {
	login *bool
	flow  fakeSignIn
}

func (f fakeLogins) For(string) ports.AgentSignIn { return f.flow }
func (f fakeLogins) HasLogin(string) bool         { return *f.login }

// failingProjects is a project store whose save fails.
type failingProjects struct{ memProjects }

func (failingProjects) PutFor(string, ports.AgentCredentialRecord) error {
	return errors.New("disk full")
}

func code(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func TestAPastedCredentialThatCannotBeSavedAfterTheLogoutSaysTheProjectWasSignedOut(t *testing.T) {
	login := true
	svc, _ := newService()
	svc.WithProjects(failingProjects{memProjects{}})
	svc.WithLogins(fakeLogins{&login, fakeSignIn{login: &login}}, nil, nil)
	_, err := svc.SetFor("p1", ports.AgentCredentialAPIKey, "sk-ant-api03-x")
	if err == nil || code(err) != "AGENT_CREDENTIAL_NOT_SAVED" || !strings.Contains(err.Error(), "signed out") {
		t.Fatalf("err = %v (code %q), want AGENT_CREDENTIAL_NOT_SAVED saying the project was signed out", err, code(err))
	}
	if login {
		t.Fatal("the logout did not run")
	}
}

func TestALogoutThatExitsCleanlyButLeavesTheLoginIsAFailedSignOut(t *testing.T) {
	login := true
	svc, _ := newService()
	svc.WithProjects(memProjects{})
	svc.WithLogins(fakeLogins{&login, fakeSignIn{login: &login, keepLogin: true}}, nil, nil)
	_, err := svc.SetFor("p1", ports.AgentCredentialAPIKey, "sk-ant-api03-x")
	if code(err) != "AGENT_SIGN_OUT_FAILED" {
		t.Fatalf("err = %v, want AGENT_SIGN_OUT_FAILED", err)
	}
	if st, _ := svc.StatusFor("p1"); st.Set {
		t.Errorf("a pasted credential was saved beside a login that stayed: %+v", st)
	}
}
