//go:build !windows

package web_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

// waitForPhase polls a project's flow until it reports phase.
func (r *projectSignInRig) waitForPhase(t *testing.T, projectID, phase string) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if _, out := apiBody(t, r.srv.URL+"/api/get_project_agent_oauth?project_id="+projectID); strings.Contains(out, `"`+phase+`"`) {
			return
		}
	}
	t.Fatalf("the flow of %s never reached %s", projectID, phase)
}

func (r *projectSignInRig) post(t *testing.T, path, body string) (int, string) {
	t.Helper()
	return apiAnswer(t, http.MethodPost, r.srv.URL+"/api/"+path, body)
}

func (r *projectSignInRig) credential(t *testing.T, projectID string) ports.ProjectCredentialStatus {
	t.Helper()
	code, body := apiBody(t, r.srv.URL+"/api/get_project_agent_credential?project_id="+url.QueryEscape(projectID))
	if code != http.StatusOK {
		t.Fatalf("get_project_agent_credential: %d %s", code, body)
	}
	for _, secret := range []string{"browsersecret", "sk-ant", "good-code", "credentials.json"} {
		if strings.Contains(body, secret) {
			t.Fatalf("the credential answer leaks %q: %s", secret, body)
		}
	}
	var st ports.ProjectCredentialStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return st
}

// Every project sign-in endpoint refuses an empty or an unknown project with
// the contract's codes, and a code that is empty with INVALID_INPUT.
func TestTheProjectSignInEndpointsAnswerTheContractsErrorCodes_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	expect := func(what string, code int, body, want string) {
		t.Helper()
		if code < 400 || !strings.Contains(body, want) {
			t.Errorf("%s answered %d %s, want %s", what, code, body, want)
		}
	}
	for _, ep := range []struct{ method, path string }{
		{http.MethodGet, "get_project_agent_oauth"},
		{http.MethodGet, "get_project_agent_auth_status"},
		{http.MethodPost, "start_project_agent_oauth"},
		{http.MethodPost, "submit_project_agent_oauth_code"},
		{http.MethodPost, "cancel_project_agent_oauth"},
		{http.MethodPost, "sign_out_project_agent_auth"},
	} {
		call := func(id string) (int, string) {
			if ep.method == http.MethodGet {
				return apiBody(t, r.srv.URL+"/api/"+ep.path+"?project_id="+id)
			}
			body := `{"project_id":"` + id + `","code":"x","kind":"api_key","value":"sk-ant-api03-x"}`
			return apiAnswer(t, ep.method, r.srv.URL+"/api/"+ep.path, body)
		}
		code, body := call("")
		expect(ep.path+" with no project", code, body, "INVALID_INPUT")
		code, body = call("no-such-project")
		expect(ep.path+" with an unknown project", code, body, "PROJECT_NOT_FOUND")
	}
}

// The contract: an empty code is INVALID_INPUT, whether or not a flow waits for
// one. With no flow it answers AGENT_OAUTH_NOT_WAITING today, as the global
// endpoint does. The same holds for an empty project_id on the three credential
// endpoints, which the port documents too ("An empty project id is refused
// with INVALID_INPUT"): they answer PROJECT_NOT_FOUND, as on main. Both are
// contract gaps the change left, not regressions.
func TestAnEmptyCodeWhileAFlowWaitsIsInvalidInput_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	r.post(t, "start_project_agent_oauth", `{"project_id":"`+r.alpha.ID+`"}`)
	r.waitForPhase(t, r.alpha.ID, "waiting_for_code")
	code, body := r.post(t, "submit_project_agent_oauth_code", `{"project_id":"`+r.alpha.ID+`","code":""}`)
	if code < 400 || !strings.Contains(body, "INVALID_INPUT") {
		t.Errorf("an empty code while the flow waits answered %d %s, want INVALID_INPUT", code, body)
	}
	if _, out := apiBody(t, r.srv.URL+"/api/get_project_agent_oauth?project_id="+r.alpha.ID); !strings.Contains(out, "waiting_for_code") {
		t.Errorf("an empty code ended the waiting flow: %s", out)
	}
	r.post(t, "cancel_project_agent_oauth", `{"project_id":"`+r.alpha.ID+`"}`)
}

func TestAnEmptyCodeWithNoFlowIsInvalidInput_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	code, body := r.post(t, "submit_project_agent_oauth_code", `{"project_id":"`+r.alpha.ID+`","code":""}`)
	if code < 400 || !strings.Contains(body, "INVALID_INPUT") {
		t.Errorf("an empty code answered %d %s, want INVALID_INPUT", code, body)
	}
}

func TestTheProjectCredentialEndpointsRefuseAnEmptyProjectWithInvalidInput_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	for _, ep := range []struct{ method, path, body string }{
		{http.MethodGet, "get_project_agent_credential?project_id=", ""},
		{http.MethodPost, "set_project_agent_credential", `{"project_id":"","kind":"api_key","value":"sk-ant-api03-x"}`},
		{http.MethodPost, "remove_project_agent_credential", `{"project_id":""}`},
	} {
		code, body := apiAnswer(t, ep.method, r.srv.URL+"/api/"+ep.path, ep.body)
		if code < 400 || !strings.Contains(body, "INVALID_INPUT") {
			t.Errorf("%s with no project answered %d %s, want INVALID_INPUT", ep.path, code, body)
		}
		if code, body = apiAnswer(t, ep.method, r.srv.URL+"/api/"+strings.Replace(ep.path, "project_id=", "project_id=no-such-project", 1), strings.Replace(ep.body, `"project_id":""`, `"project_id":"no-such-project"`, 1)); code != http.StatusNotFound || !strings.Contains(body, "PROJECT_NOT_FOUND") {
			t.Errorf("%s with an unknown project answered %d %s, want 404 PROJECT_NOT_FOUND", ep.path, code, body)
		}
	}
}

// A code submitted while no flow of the project waits for one is
// AGENT_OAUTH_NOT_WAITING: with no flow at all, with another project's flow
// waiting, and once the flow was cancelled.
func TestACodeForAProjectThatIsNotWaitingIsRefused_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	submit := func(id string) (int, string) {
		return r.post(t, "submit_project_agent_oauth_code", `{"project_id":"`+id+`","code":"good-code"}`)
	}
	if code, body := submit(r.alpha.ID); code < 400 || !strings.Contains(body, "AGENT_OAUTH_NOT_WAITING") {
		t.Errorf("no flow: %d %s", code, body)
	}
	if code, body := r.post(t, "start_project_agent_oauth", `{"project_id":"`+r.beta.ID+`"}`); code != http.StatusOK {
		t.Fatalf("start beta: %d %s", code, body)
	}
	r.waitForPhase(t, r.beta.ID, "waiting_for_code")
	if code, body := submit(r.alpha.ID); code < 400 || !strings.Contains(body, "AGENT_OAUTH_NOT_WAITING") {
		t.Errorf("another project's flow waits: %d %s", code, body)
	}
	if code, body := r.post(t, "cancel_project_agent_oauth", `{"project_id":"`+r.beta.ID+`"}`); code != http.StatusOK {
		t.Fatalf("cancel beta: %d %s", code, body)
	}
	if code, body := submit(r.beta.ID); code < 400 || !strings.Contains(body, "AGENT_OAUTH_NOT_WAITING") {
		t.Errorf("after cancel: %d %s", code, body)
	}
}

// A second start for a project whose flow runs is AGENT_OAUTH_BUSY; the flows
// of other projects and the global flow do not count, and each can run beside it.
func TestAProjectFlowIsBusyOnlyForItsOwnProject_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	start := func(id string) (int, string) {
		return r.post(t, "start_project_agent_oauth", `{"project_id":"`+id+`"}`)
	}
	if code, body := start(r.alpha.ID); code != http.StatusOK {
		t.Fatalf("start alpha: %d %s", code, body)
	}
	r.waitForPhase(t, r.alpha.ID, "waiting_for_code")
	if code, body := start(r.alpha.ID); code < 400 || !strings.Contains(body, "AGENT_OAUTH_BUSY") {
		t.Errorf("second start of alpha: %d %s", code, body)
	}
	if code, body := start(r.beta.ID); code != http.StatusOK {
		t.Errorf("alpha's flow blocks beta: %d %s", code, body)
	}
	if code, body := r.post(t, "start_agent_oauth", `{}`); code != http.StatusOK {
		t.Errorf("alpha's flow blocks the global flow: %d %s", code, body)
	}
	for _, id := range []string{r.alpha.ID, r.beta.ID} {
		r.post(t, "cancel_project_agent_oauth", `{"project_id":"`+id+`"}`)
	}
	r.post(t, "cancel_agent_oauth", `{}`)
	// Cancel frees the project: a new flow starts.
	if code, body := start(r.alpha.ID); code != http.StatusOK {
		t.Errorf("start after cancel: %d %s", code, body)
	}
}

// Cancel ends the flow and saves nothing: no login, no change to the
// effective credential, and the other project's flow runs on.
func TestCancellingAProjectFlowSavesNothingAndLeavesOthersRunning_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	r.post(t, "start_project_agent_oauth", `{"project_id":"`+r.alpha.ID+`"}`)
	r.post(t, "start_project_agent_oauth", `{"project_id":"`+r.beta.ID+`"}`)
	r.waitForPhase(t, r.alpha.ID, "waiting_for_code")
	r.waitForPhase(t, r.beta.ID, "waiting_for_code")
	if code, body := r.post(t, "cancel_project_agent_oauth", `{"project_id":"`+r.alpha.ID+`"}`); code != http.StatusOK {
		t.Fatalf("cancel: %d %s", code, body)
	}
	if r.projects.HasLogin(r.alpha.ID) {
		t.Error("a cancelled flow left a login")
	}
	if st := r.credential(t, r.alpha.ID); st.Set || st.Effective.Source != ports.EffectiveNone {
		t.Errorf("a cancelled flow changed the credential: %+v", st)
	}
	if _, out := apiBody(t, r.srv.URL+"/api/get_project_agent_oauth?project_id="+r.beta.ID); !strings.Contains(out, "waiting_for_code") {
		t.Errorf("cancelling alpha ended beta's flow: %s", out)
	}
	r.post(t, "cancel_project_agent_oauth", `{"project_id":"`+r.beta.ID+`"}`)
}

// The effective-credential line follows what is signed in: nothing, the global
// pasted credential, the global login (which beats it), and, once the global
// login is signed out, back to the pasted one; a project's own login beats all
// and leaves another project's line alone.
func TestTheEffectiveCredentialLineFollowsTheGlobalAndProjectLogins_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	src := func(id string) string { return r.credential(t, id).Effective.Source }
	if got := src(r.alpha.ID); got != ports.EffectiveNone {
		t.Fatalf("nothing signed in: %q", got)
	}
	if _, err := r.cred.Set(ports.AgentCredentialOAuth, "tok-global"); err != nil {
		t.Fatal(err)
	}
	if st := r.credential(t, r.alpha.ID); st.Effective.Source != ports.EffectiveGlobalPasted || st.Effective.Kind != ports.AgentCredentialOAuth || st.Set {
		t.Errorf("global pasted: %+v", st)
	}

	creds := filepath.Join(r.globalHome, ".credentials.json")
	if err := os.MkdirAll(r.globalHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(creds, []byte(`{"claudeAiOauth":{"accessToken":"browsersecret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st := r.credential(t, r.alpha.ID)
	if st.Effective.Source != ports.EffectiveGlobalLogin || st.Effective.Account != "ana@example.com" || st.Effective.Subscription != "max" {
		t.Errorf("global login: %+v", st.Effective)
	}

	signInThroughTheAPI(t, r.srv, r.alpha.ID)
	if st := r.credential(t, r.alpha.ID); st.Kind != ports.AgentCredentialLogin || st.Effective.Source != ports.EffectiveProject || st.Account != "ana@example.com" || !st.CheckedOK {
		t.Errorf("project login: %+v", st)
	}
	if got := src(r.beta.ID); got != ports.EffectiveGlobalLogin {
		t.Errorf("alpha's login changed beta's line to %q", got)
	}

	if code, body := r.post(t, "sign_out_agent_auth", `{}`); code != http.StatusOK {
		t.Fatalf("global sign-out: %d %s", code, body)
	}
	if got := src(r.beta.ID); got != ports.EffectiveGlobalPasted {
		t.Errorf("global login signed out, beta falls back to %q, want the global pasted credential", got)
	}
	if got := src(r.alpha.ID); got != ports.EffectiveProject {
		t.Errorf("the global sign-out changed alpha's own login to %q", got)
	}

	if _, err := r.cred.Remove(); err != nil {
		t.Fatal(err)
	}
	if got := src(r.beta.ID); got != ports.EffectiveNone {
		t.Errorf("nothing left for beta: %q", got)
	}

	if code, body := r.post(t, "sign_out_project_agent_auth", `{"project_id":"`+r.alpha.ID+`"}`); code != http.StatusOK {
		t.Fatalf("project sign-out: %d %s", code, body)
	}
	if st := r.credential(t, r.alpha.ID); st.Set || st.Effective.Source != ports.EffectiveNone {
		t.Errorf("after the project sign-out: %+v", st)
	}
}

// Pasting over a login signs that project's login out, and only that
// project's; signing in over a pasted credential removes it.
func TestPastingAndSigningInReplaceEachOtherPerProject_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	signInThroughTheAPI(t, r.srv, r.alpha.ID)
	signInThroughTheAPI(t, r.srv, r.beta.ID)

	if code, body := r.post(t, "set_project_agent_credential", `{"project_id":"`+r.alpha.ID+`","kind":"api_key","value":"sk-ant-api03-x"}`); code != http.StatusOK {
		t.Fatalf("paste: %d %s", code, body)
	} else if !strings.Contains(body, `"api_key"`) || strings.Contains(body, "sk-ant-api03-x") {
		t.Errorf("the paste answer: %s", body)
	}
	if r.projects.HasLogin(r.alpha.ID) || !r.projects.HasLogin(r.beta.ID) {
		t.Errorf("logins after the paste: alpha %v, beta %v", r.projects.HasLogin(r.alpha.ID), r.projects.HasLogin(r.beta.ID))
	}
	logs, _ := os.ReadFile(filepath.Join(filepath.Dir(r.script), "logouts"))
	if got := strings.TrimSpace(string(logs)); got != filepath.Join(r.homes, r.alpha.ID) {
		t.Errorf("logouts ran in %q, want alpha's home only", got)
	}
	if st := r.credential(t, r.alpha.ID); st.Kind != ports.AgentCredentialAPIKey || st.Effective.Source != ports.EffectiveProject || st.Effective.Kind != ports.AgentCredentialAPIKey {
		t.Errorf("alpha after the paste: %+v", st)
	}

	signInThroughTheAPI(t, r.srv, r.alpha.ID)
	if st := r.credential(t, r.alpha.ID); st.Kind != ports.AgentCredentialLogin || st.Effective.Kind != ports.AgentCredentialLogin {
		t.Errorf("alpha after signing in over the paste: %+v", st)
	}
	if st, _ := r.cred.StatusFor(r.alpha.ID); st.Set {
		t.Errorf("the pasted credential survived the sign-in: %+v", st)
	}
}
