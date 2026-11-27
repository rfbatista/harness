package app

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/bwrap"
	"operators-mcp/internal/adapter/out/personcred"
	"operators-mcp/internal/adapter/out/seatbelt"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// The folder of the person credential file is a secret of every sandbox profile (ADR 0016):
// denied after every grant in the Seatbelt profile and masked over every bind in
// the bubblewrap arguments, so no folder a session may reach shows it.

func TestEverySandboxProfileDeniesThePersonCredentialFileAfterEveryGrant(t *testing.T) {
	home := t.TempDir()
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	t.Setenv("HOME", home)
	cred, err := personcred.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The secret is the folder, so the temporary file the credential is written
	// through is covered too.
	folder := filepath.Dir(cred)
	if err := os.WriteFile(cred+".tmp", []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := harnessSecrets(Config{DBPath: ":memory:"})
	if !slices.Contains(secrets, folder) {
		t.Fatalf("%s is not among the harness secrets %v", folder, secrets)
	}

	// Every grant that could show it: the folder it lives in, and its parent, read-write and read-only.
	for name, session := range map[string]orchestration.SandboxSession{
		"the folder read-write": {Mode: "implement", Worktree: t.TempDir(), TempDir: t.TempDir(), Allow: []ports.SandboxPath{{Path: folder, Write: true}}},
		"the folder read-only":  {Mode: "implement", Worktree: t.TempDir(), TempDir: t.TempDir(), Allow: []ports.SandboxPath{{Path: folder}}},
		"the home read-write":   {Mode: "implement", Worktree: t.TempDir(), TempDir: t.TempDir(), Allow: []ports.SandboxPath{{Path: home, Write: true}}},
		"a read-only reviewer":  {Mode: "review", Worktree: home, TempDir: t.TempDir()},
		"no grant at all":       {Mode: "design", Worktree: t.TempDir(), TempDir: t.TempDir()},
	} {
		session.Secrets = secrets
		policy := orchestration.BuildSandboxPolicy(session)
		if !slices.Contains(policy.Secrets, folder) {
			t.Fatalf("%s: the policy does not carry the credential folder as a secret: %v", name, policy.Secrets)
		}

		// Seatbelt: the last rule for the file is a deny, after every allow.
		profile := seatbeltProfile(t, home, policy)
		deny := strings.LastIndex(profile, `(subpath "`+folder+`"))`)
		if deny < 0 {
			t.Errorf("%s: the Seatbelt profile has no rule for %s", name, folder)
			continue
		}
		if line := profile[strings.LastIndex(profile[:deny], "\n")+1:]; !strings.HasPrefix(line, "(deny file-read* file-write*") {
			t.Errorf("%s: the last Seatbelt rule for the folder is not a deny: %q", name, line)
		}
		for _, grant := range []string{folder, home} {
			for _, rule := range []string{`(allow file-read* file-write* (subpath "` + grant + `"))`, `(allow file-read* (subpath "` + grant + `"))`} {
				if at := strings.LastIndex(profile, rule); at > deny {
					t.Errorf("%s: %s comes after the deny, so it would reopen the file", name, rule)
				}
			}
		}

		// Bubblewrap: the file is masked after every bind.
		args := bwrap.Render(policy, home)
		mask := -1
		lastBind := -1
		for i, a := range args {
			switch {
			case a == "--tmpfs" && i+1 < len(args) && args[i+1] == folder:
				mask = i // an empty tmpfs over the folder hides the file and its temporary file
			case (a == "--bind-try" || a == "--ro-bind-try" || a == "--bind" || a == "--ro-bind") && i+1 < len(args) && args[i+1] != "/dev/null":
				lastBind = i
			}
		}
		if mask < 0 {
			t.Errorf("%s: the bubblewrap arguments do not mask the credential folder: %v", name, args)
		} else if mask < lastBind {
			t.Errorf("%s: a bind (%d) comes after the mask (%d), so it would cover it", name, lastBind, mask)
		}
	}
}

// seatbeltProfile is the Seatbelt profile text for policy, with each parameter
// replaced by its path.
func seatbeltProfile(t *testing.T, home string, policy ports.SandboxPolicy) string {
	t.Helper()
	d := &seatbelt.Driver{Home: func() (string, error) { return home, nil }}
	cmd, err := d.Wrap(policy, ports.SandboxCommand{Path: "/bin/true", Dir: policy.WorkingDir})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	var profile string
	for i, a := range cmd.Args {
		switch {
		case a == "-D" && i+1 < len(cmd.Args):
			k, v, _ := strings.Cut(cmd.Args[i+1], "=")
			values[k] = v
		case a == "-p" && i+1 < len(cmd.Args):
			profile = cmd.Args[i+1]
		}
	}
	param := regexp.MustCompile(`P\d+`)
	return regexp.MustCompile(`\(param "(P\d+)"\)`).ReplaceAllStringFunc(profile, func(m string) string {
		return strconv.Quote(values[param.FindString(m)])
	})
}

// ~/.coding-pool holds every session's worktree as well as the person's
// credential folder: denying the credential's folder must leave a session its own
// worktree, readable and writable, in both profiles.
func TestASessionsWorktreeUnderTheClientStateFolderStaysUsableWhileTheCredentialFolderIsDenied(t *testing.T) {
	home := t.TempDir()
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	t.Setenv("HOME", home)
	cred, err := personcred.Path()
	if err != nil {
		t.Fatal(err)
	}
	credDir := filepath.Dir(cred)
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, ".coding-pool")
	worktree := filepath.Join(state, "worktrees", "harness-abc", "agent-x")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	secrets := harnessSecrets(Config{DBPath: ":memory:"})
	if slices.Contains(secrets, state) {
		t.Fatalf("~/.coding-pool itself is a secret, which would deny every session its worktree: %v", secrets)
	}
	if !slices.Contains(secrets, credDir) {
		t.Fatalf("the credential folder %s is not a secret: %v", credDir, secrets)
	}

	policy := orchestration.BuildSandboxPolicy(orchestration.SandboxSession{
		Mode: "implement", Worktree: worktree, TempDir: t.TempDir(), Secrets: secrets})
	if !slices.Contains(policy.Writable, worktree) {
		t.Fatalf("the worktree is not writable in the policy: %v", policy.Writable)
	}

	// Seatbelt: the worktree is granted read-write, no rule denies ~/.coding-pool or
	// the worktree, and the credential folder is denied last.
	profile := seatbeltProfile(t, home, policy)
	grant := strings.LastIndex(profile, `(allow file-read* file-write* (subpath "`+worktree+`"))`)
	if grant < 0 {
		t.Fatalf("the worktree is not granted read-write in the Seatbelt profile:\n%s", profile)
	}
	for _, covering := range []string{state, filepath.Join(state, "worktrees"), worktree} {
		if i := strings.LastIndex(profile, `(deny file-read* file-write* (with message`); i >= 0 {
			for _, line := range strings.Split(profile[grant:], "\n") {
				if strings.HasPrefix(line, "(deny") && strings.Contains(line, `(subpath "`+covering+`")`) {
					t.Errorf("a deny after the worktree's grant covers %s: %s", covering, line)
				}
			}
		}
	}
	deny := strings.LastIndex(profile, `(subpath "`+credDir+`"))`)
	if deny < grant {
		t.Errorf("the credential folder's deny (%d) is not after the worktree's grant (%d)", deny, grant)
	}

	// Bubblewrap: the worktree is bound read-write, ~/.coding-pool is not covered by a
	// tmpfs, and the credential folder is masked after every bind.
	args := bwrap.Render(policy, home)
	bound, mask, last := false, -1, -1
	for i, a := range args {
		switch {
		case a == "--tmpfs" && i+1 < len(args) && (args[i+1] == state || args[i+1] == filepath.Join(state, "worktrees") || args[i+1] == worktree):
			t.Errorf("a tmpfs over %s hides the session's worktree: %v", args[i+1], args)
		case a == "--tmpfs" && i+1 < len(args) && args[i+1] == credDir:
			mask = i
		case (a == "--bind-try" || a == "--bind") && i+2 < len(args) && args[i+1] == worktree:
			bound = true
			last = max(last, i)
		case (a == "--bind-try" || a == "--ro-bind-try" || a == "--bind" || a == "--ro-bind") && i+1 < len(args) && args[i+1] != "/dev/null":
			last = max(last, i)
		}
	}
	if !bound {
		t.Errorf("the worktree is not bound read-write: %v", args)
	}
	if mask < 0 || mask < last {
		t.Errorf("the credential folder mask (%d) is missing or comes before a bind (%d): %v", mask, last, args)
	}
}
