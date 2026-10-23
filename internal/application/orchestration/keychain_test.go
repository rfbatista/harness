package orchestration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func hasCredentialEnv(env []string) (names []string) {
	for _, e := range env {
		for _, n := range []string{"CLAUDE_CODE_OAUTH_TOKEN=", "ANTHROPIC_API_KEY="} {
			if strings.HasPrefix(e, n) {
				names = append(names, strings.TrimSuffix(n, "="))
			}
		}
	}
	return names
}

// loginFixture is an agent home with or without a login file, read through
// the file system: no process is started to find out.
func loginFixture(t *testing.T, login bool) func() bool {
	t.Helper()
	dir := t.TempDir()
	if login {
		if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(`{"claudeAiOauth":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return func() bool {
		b, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
		return err == nil && strings.Contains(string(b), "claudeAiOauth")
	}
}

func TestALoginInTheAgentHomeKeepsThePastedCredentialOutOfAnInteractiveSessionsEnv(t *testing.T) {
	for name, tc := range map[string]struct {
		login  bool
		pasted []string
		want   []string
	}{
		"login and pasted token":   {true, []string{"CLAUDE_CODE_OAUTH_TOKEN=tok"}, nil},
		"login and pasted api key": {true, []string{"ANTHROPIC_API_KEY=key"}, nil},
		"pasted token only":        {false, []string{"CLAUDE_CODE_OAUTH_TOKEN=tok"}, []string{"CLAUDE_CODE_OAUTH_TOKEN"}},
		"pasted api key only":      {false, []string{"ANTHROPIC_API_KEY=key"}, []string{"ANTHROPIC_API_KEY"}},
		"login only":               {true, nil, nil},
		"neither":                  {false, nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			svc.AgentCredential = func() []string { return tc.pasted }
			svc.AgentLogin = loginFixture(t, tc.login)
			sess, launch := startInteractive(t, svc, InteractiveRequest{})
			if got := hasCredentialEnv(launch.Spec.Env); !slices.Equal(got, tc.want) {
				t.Fatalf("start env credentials = %v, want %v", got, tc.want)
			}
			if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
				t.Fatal(err)
			}
			_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
			if err != nil {
				t.Fatal(err)
			}
			if got := hasCredentialEnv(spec.Env); !slices.Equal(got, tc.want) {
				t.Fatalf("resume env credentials = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheLoginDecisionIsReadAtEachStartSoASignInTakesEffectAtOnce(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.AgentCredential = func() []string { return []string{"ANTHROPIC_API_KEY=key"} }
	login := false
	svc.AgentLogin = func() bool { return login }
	_, launch := startInteractive(t, svc, InteractiveRequest{})
	if len(hasCredentialEnv(launch.Spec.Env)) != 1 {
		t.Fatalf("no login: env %v", launch.Spec.Env)
	}
	login = true
	_, launch = startInteractive(t, svc, InteractiveRequest{})
	if got := hasCredentialEnv(launch.Spec.Env); len(got) != 0 {
		t.Fatalf("after sign in: %v", got)
	}
}

func TestALoginInTheAgentHomeKeepsThePastedCredentialOutOfAHeadlessSessionsEnv(t *testing.T) {
	for _, login := range []bool{true, false} {
		svc, w := newHeadlessShellService(t)
		svc.AgentCredential = func() []string { return []string{"CLAUDE_CODE_OAUTH_TOKEN=tok"} }
		svc.AgentLogin = loginFixture(t, login)
		_, launch := startHeadless(t, svc, w.task.ID)
		_, has := launch.Env["CLAUDE_CODE_OAUTH_TOKEN"]
		if has == login {
			t.Errorf("login %v: pasted token in env = %v", login, has)
		}
	}
}

// pathAfterShims checks that path is the edit-proxy shim dir (when wantShims),
// the keychain shim dir, then the rest of PATH starting with rest.
func checkKeychainShimOnPath(t *testing.T, path, kc string, wantShims bool) {
	t.Helper()
	parts := filepath.SplitList(path)
	i := slices.Index(parts, kc)
	switch {
	case i < 0:
		t.Fatalf("PATH %q lacks the keychain shim dir %q", path, kc)
	case wantShims && (i != 1 || parts[0] == ""):
		t.Fatalf("PATH %q: keychain shim dir at %d, want right after the edit-proxy shims", path, i)
	case !wantShims && i != 0:
		t.Fatalf("PATH %q: keychain shim dir at %d, want first", path, i)
	}
	if wantShims {
		if _, err := os.Readlink(filepath.Join(parts[0], "sed")); err != nil {
			t.Fatalf("first entry %q is not the edit-proxy shim dir", parts[0])
		}
	}
	if rest := strings.Join(parts[i+1:], string(os.PathListSeparator)); !strings.HasSuffix(rest, os.Getenv("PATH")) {
		t.Errorf("after the keychain shims PATH = %q, want the developer bin then the server's %q", rest, os.Getenv("PATH"))
	}
}

func TestEveryAgentStartHasTheSecurityShimAfterTheEditProxyShimsAndBeforeTheRest(t *testing.T) {
	kc := t.TempDir()
	devbin := "/dev/bin/usr/bin"
	t.Run("interactive with shims", func(t *testing.T) {
		svc, _ := newInteractiveService(t)
		w := withShells(t, svc)
		svc.ShimBinary, svc.KeychainShimDir, svc.DeveloperBin = "/opt/harness/server", kc, func() (string, error) { return devbin, nil }
		sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
		env := specEnv(launch.Spec)
		checkKeychainShimOnPath(t, env["PATH"], kc, true)
		if parts := filepath.SplitList(env["PATH"]); parts[2] != devbin {
			t.Errorf("PATH %q: the developer bin is not right after the keychain shims", env["PATH"])
		}
		if env[ports.KeychainShimDirEnv] != kc {
			t.Errorf("%s = %q", ports.KeychainShimDirEnv, env[ports.KeychainShimDirEnv])
		}
		// A resume keeps it.
		if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
			t.Fatal(err)
		}
		_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
		if err != nil {
			t.Fatal(err)
		}
		checkKeychainShimOnPath(t, specEnv(spec)["PATH"], kc, true)
	})
	t.Run("interactive without a shell or shims", func(t *testing.T) {
		svc, _ := newInteractiveService(t)
		svc.KeychainShimDir = kc
		_, launch := startInteractive(t, svc, InteractiveRequest{})
		checkKeychainShimOnPath(t, specEnv(launch.Spec)["PATH"], kc, false)
	})
	t.Run("headless", func(t *testing.T) {
		svc, w := newHeadlessShellService(t)
		svc.ShimBinary, svc.KeychainShimDir = "/opt/harness/server", kc
		_, launch := startHeadless(t, svc, w.task.ID)
		checkKeychainShimOnPath(t, launch.Path, kc, true)
	})
	t.Run("headless without a task", func(t *testing.T) {
		svc, _, _ := newTestService(t)
		svc.DefaultEnv = append(svc.DefaultEnv, "CLAUDE_FAKE_RECORD=1")
		svc.KeychainShimDir = kc
		sess, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "go"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Stop(context.Background(), sess.ID) })
		var launch fakeLaunch
		if err := json.Unmarshal(waitFile(t, filepath.Join(sess.WorkingDir, fakeLaunchFile)), &launch); err != nil {
			t.Fatal(err)
		}
		checkKeychainShimOnPath(t, launch.Path, kc, false)
	})
}

func TestTheSecurityShimIsOnThePathWithTheSandboxOnAndOffAndItsDirIsReadable(t *testing.T) {
	kc := t.TempDir()
	for _, off := range []bool{false, true} {
		w := newSandboxWorld(t)
		w.off = off
		w.svc.KeychainShimDir = kc
		_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", RunsOn: domain.RunnerTUI})
		if err != nil {
			t.Fatal(err)
		}
		checkKeychainShimOnPath(t, specEnv(spec)["PATH"], kc, false)
		calls := w.driver.Calls()
		if off != (len(calls) == 0) {
			t.Fatalf("sandbox off=%v but the driver was called %d times", off, len(calls))
		}
		if !off && !slices.Contains(calls[0].Policy.Readable, kc) {
			t.Errorf("the keychain shim dir %q is not readable in the policy: %v", kc, calls[0].Policy.Readable)
		}
	}
}

func TestWithoutAKeychainShimDirNothingIsAddedToPath(t *testing.T) {
	svc, _ := newInteractiveService(t)
	w := withShells(t, svc)
	svc.ShimBinary = "/opt/harness/server"
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	env := specEnv(launch.Spec)
	if _, ok := env[ports.KeychainShimDirEnv]; ok || strings.Contains(env["PATH"], "keychain") {
		t.Fatalf("env %v", env["PATH"])
	}
}

// A login shell whose profile reorders PATH still finds the security shim
// right after the edit-proxy shims.
func TestALoginShellKeepsTheSecurityShimAfterTheEditProxyShims(t *testing.T) {
	svc := newLoginHostedService(t, scriptAgent{script: `printf '%s' "$PATH" > path; printf 'ready\r\n'; cat`})
	w := withShells(t, svc)
	kc := t.TempDir()
	svc.ShimBinary, svc.KeychainShimDir = "/opt/harness/server", kc
	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID,
		RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 80, Rows: 24}})
	path := string(waitFile(t, filepath.Join(sess.WorkingDir, "path")))
	checkKeychainShimOnPath(t, path, kc, true)
	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
}
