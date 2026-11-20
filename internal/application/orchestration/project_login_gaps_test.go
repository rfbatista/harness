package orchestration

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A resume decides the effective credential again (ADR 0102, spec: "decided at
// each start and resume"): a project that switched kind between the start and
// the resume resumes on the new one, with no trace of the old.
func TestAnInteractiveResumeFollowsAProjectThatSwitchedItsCredentialSinceTheStart(t *testing.T) {
	type state struct {
		login        bool
		pasted       []string
		agentLogin   bool
		globalPasted []string
	}
	pastedKey := []string{"ANTHROPIC_API_KEY=pk"}
	globalTok := []string{"CLAUDE_CODE_OAUTH_TOKEN=gt"}
	for name, tc := range map[string]struct {
		start, resume state
		// what the resume must get
		wantConfigDir  bool
		wantCredential []string
	}{
		"login to pasted":                  {state{login: true}, state{pasted: pastedKey}, false, []string{"ANTHROPIC_API_KEY"}},
		"pasted to login":                  {state{pasted: pastedKey}, state{login: true}, true, nil},
		"login to signed out, global only": {state{login: true, globalPasted: globalTok}, state{globalPasted: globalTok}, false, []string{"CLAUDE_CODE_OAUTH_TOKEN"}},
		"login to signed out, nothing":     {state{login: true}, state{}, false, nil},
		"pasted removed, global pasted":    {state{pasted: pastedKey, globalPasted: globalTok}, state{globalPasted: globalTok}, false, []string{"CLAUDE_CODE_OAUTH_TOKEN"}},
		"nothing to project login":         {state{globalPasted: globalTok}, state{login: true, globalPasted: globalTok}, true, nil},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			home := t.TempDir()
			cur := tc.start
			svc.ProjectLoginHome = func(id string) string {
				if id == "p1" && cur.login {
					return home
				}
				return ""
			}
			svc.ProjectAgentCredential = func(string) ([]string, bool) { return cur.pasted, len(cur.pasted) > 0 }
			svc.AgentCredential = func() []string { return cur.globalPasted }
			svc.AgentLogin = func() bool { return cur.agentLogin }

			sess, first := startInteractive(t, svc, InteractiveRequest{})
			if (first.Spec.ConfigDir == home) != tc.start.login {
				t.Fatalf("start: config dir %q, project login %v", first.Spec.ConfigDir, tc.start.login)
			}
			if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
				t.Fatal(err)
			}
			cur = tc.resume
			_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
			if err != nil {
				t.Fatal(err)
			}
			env := launchOf(t, spec).Env
			dir, has := configDirOf(env)
			if tc.wantConfigDir != (spec.ConfigDir == home && has && dir == home) {
				t.Errorf("resume: config dir %q (spec %q), want the project Claude home: %v", dir, spec.ConfigDir, tc.wantConfigDir)
			}
			if !tc.wantConfigDir && (spec.ConfigDir != "" || has && dir == home) {
				t.Errorf("resume still carries the project Claude home: spec %q, env %q", spec.ConfigDir, dir)
			}
			if got := hasCredentialEnv(env); !slices.Equal(got, tc.wantCredential) {
				t.Errorf("resume credential env %v, want %v", got, tc.wantCredential)
			}
		})
	}
}

// Another project's login never reaches a session: with both projects signed
// in, the session of p1 runs in p1's Claude home, is granted that home alone,
// and the folder that holds the others stays hidden, at start and at resume.
func TestSandbox_ASessionOfASignedInProjectIsGrantedItsHomeAloneAtStartAndResume(t *testing.T) {
	w := newSandboxWorld(t)
	root := w.projectHomes(t, "p1", "p2")
	w.svc.Sandbox.ProjectHasLogin = func(string) bool { return true }
	w.svc.ProjectLoginHome = func(id string) string { return filepath.Join(root, id) }

	check := func(when string, spec ports.AgentSpec, p ports.SandboxPolicy) {
		t.Helper()
		if spec.ConfigDir != filepath.Join(root, "p1") {
			t.Errorf("%s: config dir %q, want p1's home", when, spec.ConfigDir)
		}
		if !slices.Contains(p.Writable, spec.ConfigDir) {
			t.Errorf("%s: the config dir %s is not writable under the policy: %v", when, spec.ConfigDir, p.Writable)
		}
		for _, list := range [][]string{p.Readable, p.Writable} {
			if slices.Contains(list, filepath.Join(root, "p2")) || slices.Contains(list, root) {
				t.Errorf("%s: p2's home or the homes folder is granted: %v", when, list)
			}
		}
		if !slices.Contains(p.Hidden, root) {
			t.Errorf("%s: the homes folder is not hidden: %v", when, p.Hidden)
		}
	}
	sess, spec0, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: "implement", TicketID: "tk1", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	calls := w.driver.Calls()
	check("start", spec0, calls[len(calls)-1].Policy)

	if _, err := w.svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	before := len(w.driver.Calls())
	_, spec, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	calls = w.driver.Calls()
	if len(calls) <= before {
		t.Fatal("the resume did not go through the sandbox")
	}
	check("resume", spec, calls[len(calls)-1].Policy)
}

// A project that signs out between start and resume resumes with the policy it
// got before it had a login: its home is no longer granted.
func TestSandbox_AResumeAfterSignOutNoLongerGrantsTheProjectHome(t *testing.T) {
	w := newSandboxWorld(t)
	root := w.projectHomes(t, "p1")
	login := true
	w.svc.Sandbox.ProjectHasLogin = func(string) bool { return login }
	w.svc.ProjectLoginHome = func(id string) string {
		if login {
			return filepath.Join(root, id)
		}
		return ""
	}
	sess, p := w.start(t, "implement")
	if !slices.Contains(p.Writable, filepath.Join(root, "p1")) {
		t.Fatalf("signed in: home not writable: %v", p.Writable)
	}
	if _, err := w.svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	login = false
	_, spec, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	calls := w.driver.Calls()
	if got := calls[len(calls)-1].Policy; slices.Contains(got.Writable, filepath.Join(root, "p1")) {
		t.Errorf("a signed-out project's home is still writable on resume: %v", got.Writable)
	}
	if spec.ConfigDir != "" {
		t.Errorf("resume config dir %q, want none", spec.ConfigDir)
	}
}
