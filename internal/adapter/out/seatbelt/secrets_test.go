package seatbelt

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// Hidden secrets (ADR 0016) are denied after every grant, because the last rule
// of a Seatbelt profile wins: a database that sits inside a repository the
// session may read must stay unreadable.

// rendered answers the profile text of a wrapped command, with each (param "Pn")
// replaced by its path, so a test reads the rules as paths.
func rendered(t *testing.T, p ports.SandboxPolicy) string {
	t.Helper()
	home := t.TempDir()
	d := &Driver{Home: func() (string, error) { return home, nil }}
	cmd, err := d.Wrap(p, ports.SandboxCommand{Path: "/bin/true", Dir: p.WorkingDir})
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
	return regexp.MustCompile(`\(param "(P\d+)"\)`).ReplaceAllStringFunc(profile, func(m string) string {
		return strconv.Quote(values[regexp.MustCompile(`P\d+`).FindString(m)])
	})
}

func TestASecretInsideAGrantedRepositoryIsDeniedAfterTheGrantThatWouldReopenIt(t *testing.T) {
	repo := t.TempDir()
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		repo = real
	}
	db := filepath.Join(repo, "data.db")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := rendered(t, ports.SandboxPolicy{
		WorkingDir: repo,
		Readable:   []string{repo},
		Secrets:    []string{db, db + "-wal"},
	})
	grant := strings.LastIndex(profile, `(allow file-read* (subpath "`+repo+`"))`)
	deny := strings.LastIndex(profile, `(subpath "`+db+`"))`)
	if deny >= 0 && !strings.HasPrefix(profile[strings.LastIndex(profile[:deny], "\n")+1:], "(deny file-read* file-write*") {
		t.Fatalf("the last rule for the secret is not a deny of reads and writes:\n%s", profile)
	}
	if grant < 0 || deny < 0 {
		t.Fatalf("grant %d deny %d in:\n%s", grant, deny, profile)
	}
	if deny < grant {
		t.Errorf("the secret is denied before the grant that reopens it, so the last rule lets it through:\n%s", profile)
	}
	if !strings.Contains(profile, `(subpath "`+db+`-wal")`) {
		t.Errorf("the write-ahead log is not denied:\n%s", profile)
	}
}

func TestAPolicyWithNoSecretsRendersNoSecretRules(t *testing.T) {
	dir := t.TempDir()
	profile := rendered(t, ports.SandboxPolicy{WorkingDir: dir, Writable: []string{dir}})
	if strings.Count(profile, "(deny file-read* file-write*") != 1 {
		t.Errorf("an extra deny without secrets:\n%s", profile)
	}
}
