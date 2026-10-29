package vshell

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestPersonEnvRemovesEveryHarnessEntryAndKeepsTheRest(t *testing.T) {
	shims := "/tmp/harness-shims-123"
	saver := "/tmp/savers/a:/tmp/savers/b"
	keychain := "/tmp/harness-keychain-shim-9"
	devbin := "/Applications/Xcode.app/Contents/Developer/usr/bin"
	server := []string{
		"PATH=" + strings.Join([]string{shims, "/tmp/savers/a", "/tmp/savers/b", keychain, devbin, "/usr/bin", "/opt/homebrew/bin"}, ":"),
		"HARNESS_SHIM_DIR=" + shims,
		"HARNESS_SAVER_BIN_DIRS=" + saver,
		"HARNESS_KEYCHAIN_SHIM_DIR=" + keychain,
		"HARNESS_DEVELOPER_BIN=" + devbin,
		"HARNESS_SHELL=/tmp/harness-shell-1",
		"HARNESS_SANDBOX=seatbelt",
		"HARNESS_ANYTHING_NEW=1",
		"CODING_POOL_HOME=/x",
		"CLAUDE_CONFIG_DIR=/agent/home",
		"CLAUDE_CODE_OAUTH_TOKEN=tok",
		"ANTHROPIC_API_KEY=k",
		"DISABLE_AUTOUPDATER=1",
		"MAC_CHROMIUM_TMPDIR=/tmp/c",
		"PWTEST_SOCKETS_DIR=/tmp/p",
		"SSH_AUTH_SOCK=/tmp/agent.sock",
		"LANG=pt_BR.UTF-8",
		"TERM=dumb",
		"HOME=/Users/me",
	}
	got := PersonEnv(server)

	for _, e := range got {
		name, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(name, "HARNESS_") || strings.HasPrefix(name, "CODING_POOL_") {
			t.Errorf("%s left in the person's env", name)
		}
	}
	has := func(kv string) bool { return slices.Contains(got, kv) }
	for _, gone := range []string{"CLAUDE_CONFIG_DIR=/agent/home", "CLAUDE_CODE_OAUTH_TOKEN=tok", "ANTHROPIC_API_KEY=k", "DISABLE_AUTOUPDATER=1", "MAC_CHROMIUM_TMPDIR=/tmp/c", "PWTEST_SOCKETS_DIR=/tmp/p", "TERM=dumb"} {
		if has(gone) {
			t.Errorf("%s left in the person's env", gone)
		}
	}
	for _, kept := range []string{"SSH_AUTH_SOCK=/tmp/agent.sock", "LANG=pt_BR.UTF-8", "HOME=/Users/me", "TERM=xterm-256color", "PATH=/usr/bin:/opt/homebrew/bin"} {
		if !has(kept) {
			t.Errorf("%s missing from %v", kept, got)
		}
	}
}

// What the agent start code sets must be removed from the person's env: a
// variable added there and not listed in personEnvRemoved fails this test.
func TestPersonEnvListsEveryVariableAgentsAreGiven(t *testing.T) {
	// Set for agents but harmless to keep when the server had it: the person
	// chose these (or the OS did), and agents get a different value only in
	// their own process. BROWSER belongs to the OAuth login subprocess only.
	kept := []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "DO_NOT_TRACK", "CODEGRAPH_TELEMETRY", "RTK_TELEMETRY_DISABLED", "GIT_COMMITTER_DATE", "BROWSER"}
	upper := regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)
	assign := regexp.MustCompile(`^([A-Z][A-Z0-9_]+)=`)
	seen := map[string]string{}
	for _, dir := range []string{
		"../orchestration", "../agentcredential", "../agentoauth", "../../adapter/out/agents/claudecli",
		"../../adapter/out/agents/claudehome", "../../ports",
	} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.ValueSpec: // const FooEnv = "NAME"
					for i, id := range n.Names {
						if i >= len(n.Values) || !(strings.HasSuffix(id.Name, "Env") || strings.HasPrefix(id.Name, "Env")) {
							continue
						}
						if lit, ok := n.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if v, _ := strconv.Unquote(lit.Value); upper.MatchString(v) {
								seen[v] = f + " " + id.Name
							}
						}
					}
				case *ast.BasicLit: // "NAME=value"
					if n.Kind == token.STRING {
						if v, _ := strconv.Unquote(n.Value); assign.MatchString(v) {
							seen[assign.FindStringSubmatch(v)[1]] = f
						}
					}
				}
				return true
			})
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no agent variables: the scan is broken")
	}
	for name, where := range seen {
		if slices.Contains(kept, name) {
			continue
		}
		got := PersonEnv([]string{name + "=x", "KEEP=1"})
		if slices.Contains(got, name+"=x") {
			t.Errorf("%s (%s) is set for agents but not removed from the person's env: add it to personEnvRemoved in personenv.go, or to kept here with a reason", name, where)
		}
	}
}
