package apps_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// q single-quotes s for the sh command lines the tests run.
func q(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// A run's environment has every root .env variable, read as dotenv: a $(…)
// or a backtick reaches the process as text and runs nothing. A nested env
// file is written but not exported.
func TestARunsEnvironmentHasTheRootDotenvVariables(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "pwned")
	subst := "$(touch " + marker + ")"
	tick := "`touch " + marker + "`"
	saved := envFiles{"r1": {
		{RepositoryID: "r1", Path: ".env", Content: "# settings\nexport GREETING=hello world # inline\n" +
			"SUBST=" + subst + "\nTICK='" + tick + "'\nQUOTED=\"a \\\"b\\\" $HOME\"\n"},
		{RepositoryID: "r1", Path: "web/.env", Content: "NESTED=web\n"},
	}}
	svc, _ := newGitRunner(t, saved)
	check := `printenv GREETING; ` +
		`[ "$SUBST" = ` + q(subst) + ` ] && echo subst-literal; ` +
		`[ "$TICK" = ` + q(tick) + ` ] && echo tick-literal; ` +
		`[ "$QUOTED" = ` + q(`a "b" $HOME`) + ` ] && echo quoted-ok; ` +
		`printenv NESTED || echo no-nested; ` +
		`env | grep -c HARNESS_DOTENV_ ; echo :end; sleep 30`
	r, err := svc.Start(context.Background(), "s1", "", check)
	if err != nil {
		t.Fatal(err)
	}
	screen := readUntil(t, svc, r.ID, ":end")
	var lines []string
	for _, l := range strings.Split(screen, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	// The last 0 counts the prefixed carriers left behind: none.
	want := []string{"hello world", "subst-literal", "tick-literal", "quoted-ok", "no-nested", "0", ":end"}
	if !slices.Equal(lines, want) {
		t.Errorf("the run shows %q, want %q", lines, want)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a value of the .env ran as a command")
	}
}

// Precedence, lowest first: the login profile, the .env, the session's git
// credentials.
func TestTheDotenvOverridesTheProfileButNotTheGitCredentials(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("export CLASH=profile ONLY_PROFILE=profile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	saved := envFiles{"r1": {{RepositoryID: "r1", Path: ".env", Content: "CLASH=dotenv\nHARNESS_FROM_ENV=dotenv\nHARNESS_FROM_FILE=dotenv\n"}}}
	svc, _ := newGitRunner(t, saved)
	envFile := filepath.Join(t.TempDir(), "env.sh")
	// As the git credentials' env file is: Env again, after the profile.
	if err := os.WriteFile(envFile, []byte("export HARNESS_FROM_ENV=env HARNESS_FROM_FILE=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc.GitAuth = &gitAuth{envFile: envFile}
	r, err := svc.Start(context.Background(), "s1", "", `printf 'env:%s-%s-%s-%s\n' "$CLASH" "$ONLY_PROFILE" "$HARNESS_FROM_ENV" "$HARNESS_FROM_FILE"; sleep 30`)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, svc, r.ID, "env:dotenv-profile-env-file")
}

// A repository without a root .env runs as before, nested env files or not.
func TestARunWithoutARootDotenvRunsNormally(t *testing.T) {
	for name, files := range map[string][]*domain.EnvFile{
		"no env files":     nil,
		"only nested ones": {{RepositoryID: "r1", Path: "api/.env", Content: "NESTED=api\n"}},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _ := newGitRunner(t, envFiles{"r1": files})
			r, err := svc.Start(context.Background(), "s1", "", `printf 'nested:%s:\n' "$NESTED"; sleep 30`)
			if err != nil {
				t.Fatal(err)
			}
			readUntil(t, svc, r.ID, "nested::")
		})
	}
}

// screenLines is the run's log as trimmed, non-empty lines.
func screenLines(screen string) []string {
	var lines []string
	for _, l := range strings.Split(screen, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// Lines of the .env that are not KEY=VALUE are skipped: the run still starts
// with the valid ones, and one note naming the line numbers, never a value,
// comes before the command's own output.
func TestSkippedDotenvLinesAreNotedBeforeTheCommandsOutput(t *testing.T) {
	const secret = "sk-live-9f8e7d6c5b4a"
	for name, tc := range map[string]struct{ dotenv, note string }{
		"several": {"GOOD=yes\nnot a line\n# comment\nAPI_TOKEN " + secret + "\nOTHER=2\n", ".env: skipped lines 2, 4 (not KEY=VALUE)"},
		"one":     {"GOOD=yes\n" + secret + "\nOTHER=2\n", ".env: skipped line 2 (not KEY=VALUE)"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _ := newGitRunner(t, envFiles{"r1": {{RepositoryID: "r1", Path: ".env", Content: tc.dotenv}}})
			r, err := svc.Start(context.Background(), "s1", "", `printf 'out:%s-%s\n' "$GOOD" "$OTHER"; sleep 30`)
			if err != nil {
				t.Fatal(err)
			}
			screen := readUntil(t, svc, r.ID, "out:yes-2")
			if want := []string{tc.note, "out:yes-2"}; !slices.Equal(screenLines(screen), want) {
				t.Errorf("the run shows %q, want %q", screenLines(screen), want)
			}
			if strings.Contains(screen, secret) {
				t.Errorf("the run's log shows a skipped line's text: %q", screen)
			}
		})
	}
}

// A .env whose every line is valid adds nothing to the run's log.
func TestAValidDotenvWritesNoNote(t *testing.T) {
	svc, _ := newGitRunner(t, envFiles{"r1": {{RepositoryID: "r1", Path: ".env", Content: "# c\n\nGOOD=yes\n"}}})
	r, err := svc.Start(context.Background(), "s1", "", `printf 'out:%s\n' "$GOOD"; sleep 30`)
	if err != nil {
		t.Fatal(err)
	}
	screen := readUntil(t, svc, r.ID, "out:yes")
	if want := []string{"out:yes"}; !slices.Equal(screenLines(screen), want) {
		t.Errorf("the run shows %q, want %q", screenLines(screen), want)
	}
}
