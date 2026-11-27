//go:build !windows

package app

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/ports"
)

// Person-only routes (ADR 0016) as the server wires them: refused without the
// credential a pairing gives, and the refusal reaches the person once an hour.

// personServer serves the API the way registerHTTPServer does for person proof:
// the real pairing service and the real refusal reporter over the booted app.
func (e *shellE2E) personServer() *httptest.Server {
	e.t.Helper()
	router := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Sessions: e.orch, Planning: e.plan, GateRuns: e.gateRuns, Pairing: e.proof, Settings: e.cat.Settings, Projects: e.cat.Projects,
	}), httpapi.WithPersonProof(e.proof, personRefusals{events: e.cat.Bus, sessions: e.sessions}))
	srv := httptest.NewServer(router)
	e.t.Cleanup(srv.Close)
	return srv
}

func personCall(t *testing.T, c *http.Client, method, url string, header map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := new(strings.Builder)
	b := make([]byte, 4096)
	for {
		n, err := res.Body.Read(b)
		buf.Write(b[:n])
		if err != nil {
			break
		}
	}
	return res.StatusCode, buf.String()
}

func TestTheServerRefusesAnUnpairedCallerOnEveryPersonOnlyRouteAcceptsAPairedOneAndTellsThePersonOncePerHour(t *testing.T) {
	e := newShellE2E(t)
	srv := e.personServer()
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	anyone := &http.Client{}

	// Everything a session has: the loopback address, the API shape and any header.
	person := map[string]string{
		"/api/sessions/s1/gate-runs":                   http.MethodPost,
		"/api/projects/" + e.project + "/agent-system": http.MethodPatch,
		"/api/branch_requests/b1/answer":               http.MethodPost,
		"/api/service_requests/r1/answer":              http.MethodPost,
		"/api/answer_held_action":                      http.MethodPost,
		"/api/approve_instruction_proposal":            http.MethodPost,
		"/api/update_settings":                         http.MethodPost,
		"/api/set_sandbox_enabled":                     http.MethodPost,
	}
	for path, method := range person {
		code, body := personCall(t, anyone, method, srv.URL+path, map[string]string{"X-Harness-Session": "mine", "Origin": srv.URL})
		if code != http.StatusForbidden || !strings.Contains(body, "PERSON_ONLY") {
			t.Errorf("%s %s unpaired = %d %s", method, path, code, body)
		}
	}

	// The refusals tell the person once: the same route twice in the hour is one notification.
	deadline := time.Now().Add(5 * time.Second)
	var notices int
	for time.Now().Before(deadline) {
		page, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{ProjectID: e.project})
		if err != nil {
			t.Fatal(err)
		}
		notices = 0
		for _, n := range page.Notifications {
			if strings.Contains(n.Title, "refused a call that was yours alone") {
				notices++
			}
		}
		if notices > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if notices != 1 {
		t.Errorf("%d notifications for a refused route that names a project, want 1", notices)
	}
	for i := 0; i < 3; i++ {
		personCall(t, anyone, http.MethodPatch, srv.URL+"/api/projects/"+e.project+"/agent-system", nil)
	}
	page, _ := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{ProjectID: e.project})
	again := 0
	for _, n := range page.Notifications {
		if strings.Contains(n.Title, "refused a call that was yours alone") {
			again++
		}
	}
	if again != 1 {
		t.Errorf("repeated refusals made %d notifications, want still 1", again)
	}

	// The harness prints a link once; opening it pairs the browser.
	code, ok := e.proof.FirstCode(e.ctx)
	if !ok {
		t.Fatal("no first code with nobody paired")
	}
	if status, body := personCall(t, browser, http.MethodGet, srv.URL+httpapi.PairPath+"?code=WRONG", nil); status != http.StatusForbidden {
		t.Errorf("a wrong code = %d %s", status, body)
	}
	if status, _ := personCall(t, browser, http.MethodGet, srv.URL+httpapi.PairPath+"?code="+code, nil); status != http.StatusSeeOther {
		t.Fatalf("pairing = %d", status)
	}
	if status, _ := personCall(t, browser, http.MethodGet, srv.URL+httpapi.PairPath+"?code="+code, nil); status != http.StatusForbidden {
		t.Errorf("the link worked twice: %d", status)
	}
	u, _ := url.Parse(srv.URL)
	if len(jar.Cookies(u)) != 1 {
		t.Fatalf("the browser holds %d cookies", len(jar.Cookies(u)))
	}
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	if status, body := personCall(t, browser, http.MethodPost, srv.URL+"/api/sessions/"+sess.ID+"/gate-runs", nil); status != http.StatusOK {
		t.Errorf("the paired browser's gate-run trigger = %d %s", status, body)
	}
	if status, _ := personCall(t, anyone, http.MethodPost, srv.URL+"/api/sessions/"+sess.ID+"/gate-runs", nil); status != http.StatusForbidden {
		t.Errorf("an unpaired caller after the pairing = %d", status)
	}

	// A paired client that is not a browser sends the credential as a header.
	status, body := personCall(t, anyone, http.MethodPost, srv.URL+httpapi.PairingCodesPath, nil)
	if status != http.StatusForbidden {
		t.Errorf("a new pairing code with no credential = %d %s", status, body)
	}
}
