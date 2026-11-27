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
