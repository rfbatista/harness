package orchestration

import (
	"context"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// credentialCase is one effective-credential case of a project (ADR 0102): what
// the project holds, what the agent home and the global credential hold, and
// what its next claude process must get.
type credentialCase struct {
	projectLogin   bool
	projectPasted  []string
	agentLogin     bool
	globalPasted   []string
	wantConfigDir  bool     // the project Claude home is the config dir
	wantCredential []string // credential env names
}

var credentialCases = map[string]credentialCase{
	"project login beats everything": {true, []string{"ANTHROPIC_API_KEY=pk"}, true, []string{"CLAUDE_CODE_OAUTH_TOKEN=gt"}, true, nil},
	"project login, nothing else":    {true, nil, false, nil, true, nil},
	"project pasted credential":      {false, []string{"ANTHROPIC_API_KEY=pk"}, true, []string{"CLAUDE_CODE_OAUTH_TOKEN=gt"}, false, []string{"ANTHROPIC_API_KEY"}},
	"global login":                   {false, nil, true, []string{"CLAUDE_CODE_OAUTH_TOKEN=gt"}, false, nil},
	"global pasted credential":       {false, nil, false, []string{"CLAUDE_CODE_OAUTH_TOKEN=gt"}, false, []string{"CLAUDE_CODE_OAUTH_TOKEN"}},
	"none":                           {false, nil, false, nil, false, nil},
}

// wire sets the service's credential rules to the case; the project Claude
// home is the path it answers.
func (c credentialCase) wire(t *testing.T, svc *Service) (home string) {
	t.Helper()
	home = t.TempDir()
	svc.ProjectLoginHome = func(projectID string) string {
		if projectID == "p1" && c.projectLogin {
			return home
		}
		return ""
	}
	svc.ProjectAgentCredential = func(string) ([]string, bool) { return c.projectPasted, len(c.projectPasted) > 0 }
	svc.AgentCredential = func() []string { return c.globalPasted }
	svc.AgentLogin = loginFixture(t, c.agentLogin)
	return home
}

// configDirOf is the CLAUDE_CONFIG_DIR a process gets: the last entry wins, as
// in an exec'd environment.
func configDirOf(env []string) (dir string, ok bool) {
	for _, e := range env {
		if v, found := strings.CutPrefix(e, "CLAUDE_CONFIG_DIR="); found {
			dir, ok = v, true
		}
	}
	return dir, ok
}

func TestAnInteractiveSessionAndItsResumeRunOnTheProjectsEffectiveCredential(t *testing.T) {
	for name, tc := range credentialCases {
		t.Run(name, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			home := tc.wire(t, svc)
			sess, launch := startInteractive(t, svc, InteractiveRequest{})
			check := func(when string, spec ports.AgentSpec, env []string) {
				t.Helper()
				dir, has := configDirOf(env)
				if tc.wantConfigDir && (!has || dir != home || spec.ConfigDir != home) {
					t.Errorf("%s: config dir %q (spec %q), want the project Claude home %q", when, dir, spec.ConfigDir, home)
				}
				if !tc.wantConfigDir && spec.ConfigDir != "" {
					t.Errorf("%s: config dir %q, want none", when, spec.ConfigDir)
				}
				if got := hasCredentialEnv(env); !slices.Equal(got, tc.wantCredential) {
					t.Errorf("%s: credential env %v, want %v", when, got, tc.wantCredential)
				}
			}
			check("start", launch.Spec, launch.Env)

			if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
				t.Fatal(err)
			}
			_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
			if err != nil {
				t.Fatal(err)
			}
			check("resume", spec, launchOf(t, spec).Env)
		})
	}
}

func TestAHeadlessSessionRunsOnTheProjectsEffectiveCredential(t *testing.T) {
	for name, tc := range credentialCases {
		t.Run(name, func(t *testing.T) {
			svc, w := newHeadlessShellService(t)
			home := tc.wire(t, svc)
			_, launch := startHeadless(t, svc, w.task.ID)
			dir, has := launch.Env["CLAUDE_CONFIG_DIR"]
			if tc.wantConfigDir != (has && dir == home) {
				t.Errorf("config dir %q (set %v), want the project Claude home %q: %v", dir, has, home, tc.wantConfigDir)
			}
			var got []string
			for _, n := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"} {
				if _, ok := launch.Env[n]; ok {
					got = append(got, n)
				}
			}
			if !slices.Equal(got, tc.wantCredential) {
				t.Errorf("credential env %v, want %v", got, tc.wantCredential)
			}
		})
	}
}

func TestAProjectLoginIsDecidedAtEachStartSoASignInOrOutTakesEffectForTheNextOne(t *testing.T) {
	svc, _ := newInteractiveService(t)
	home := t.TempDir()
	login := false
	svc.ProjectLoginHome = func(string) string {
		if login {
			return home
		}
		return ""
	}
	_, first := startInteractive(t, svc, InteractiveRequest{})
	login = true
	_, second := startInteractive(t, svc, InteractiveRequest{})
	login = false
	_, third := startInteractive(t, svc, InteractiveRequest{})
	if first.Spec.ConfigDir != "" || second.Spec.ConfigDir != home || third.Spec.ConfigDir != "" {
		t.Errorf("config dirs %q, %q, %q", first.Spec.ConfigDir, second.Spec.ConfigDir, third.Spec.ConfigDir)
	}
}
