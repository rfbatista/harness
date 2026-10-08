package ghcli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeGH is a stand-in gh binary: it records its arguments and stdin, and
// answers with the stdout, stderr and exit code the test scripted.
type fakeGH struct {
	t   *testing.T
	dir string
	bin string
}

func newFakeGH(t *testing.T) *fakeGH {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	script := `#!/bin/sh
d="` + dir + `"
printf '%s\n' "$@" > "$d/args"
cat > "$d/stdin"
env > "$d/env"
[ -f "$d/stdout" ] && cat "$d/stdout"
[ -f "$d/stderr" ] && cat "$d/stderr" >&2
exit $(cat "$d/code" 2>/dev/null || echo 0)
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeGH{t: t, dir: dir, bin: bin}
}

func (f *fakeGH) answer(stdout, stderr string, code int) {
	f.t.Helper()
	for name, body := range map[string]string{"stdout": stdout, "stderr": stderr, "code": strconv.Itoa(code)} {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fakeGH) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(f.dir, name))
	return string(b)
}

func (f *fakeGH) args() []string { return strings.Split(strings.TrimRight(f.read("args"), "\n"), "\n") }

var widgets = domain.GitRemote{Host: "github.com", Owner: "acme", Repo: "widgets"}

const prJSON = `{"number":12,"html_url":"https://github.com/acme/widgets/pull/12","title":"Add PRs","state":"open",
"draft":true,"merged_at":null,"created_at":"2026-10-07T12:00:00Z","head":{"ref":"agent/x"},"base":{"ref":"main"}}`

func TestReady(t *testing.T) {
	ctx := context.Background()
	f := newFakeGH(t)
	h := &Host{Bin: f.bin}

	f.answer("gho_xxx\n", "", 0)
	if err := h.Ready(ctx); err != nil {
		t.Fatalf("signed in: %v", err)
	}
	if got := strings.Join(f.args(), " "); got != "auth token --hostname github.com" {
		t.Fatalf("args = %q", got)
	}

	f.answer("", "no oauth token found for github.com", 1)
	err := h.Ready(ctx)
	if errs.Code(err) != domain.CodePRToolUnavailable || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("signed out = %v, want PR_TOOL_UNAVAILABLE saying to run gh auth login", err)
	}

	missing := &Host{Bin: filepath.Join(t.TempDir(), "no-gh")}
	err = missing.Ready(ctx)
	if errs.Code(err) != domain.CodePRToolUnavailable || !strings.Contains(err.Error(), "install") {
		t.Fatalf("missing gh = %v, want PR_TOOL_UNAVAILABLE saying to install it", err)
	}
}

func TestOpenSendsThePullRequestAndReadsTheAnswer(t *testing.T) {
	f := newFakeGH(t)
	h := &Host{Bin: f.bin}
	f.answer(prJSON, "", 0)

	got, err := h.Open(context.Background(), widgets, ports.OpenPullRequest{Head: "agent/x", Base: "main", Title: "Add PRs", Body: "Why\n- a", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	want := ports.HostPullRequest{Number: 12, URL: "https://github.com/acme/widgets/pull/12", Title: "Add PRs", Head: "agent/x",
		Base: "main", Draft: true, State: domain.PullRequestOpen, CreatedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	if *got != want {
		t.Fatalf("got %+v\nwant %+v", *got, want)
	}
	if args := strings.Join(f.args(), " "); args != "api --hostname github.com -X POST repos/acme/widgets/pulls --input -" {
		t.Fatalf("args = %q", args)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(f.read("stdin")), &sent); err != nil {
		t.Fatalf("stdin %q: %v", f.read("stdin"), err)
	}
	if sent["head"] != "agent/x" || sent["base"] != "main" || sent["title"] != "Add PRs" || sent["body"] != "Why\n- a" || sent["draft"] != true {
		t.Fatalf("sent = %v", sent)
	}
	env := f.read("env")
	for _, kv := range []string{"GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1"} {
		if !strings.Contains(env, kv) {
			t.Errorf("gh ran without %s", kv)
		}
	}
}

func TestOpenRefusals(t *testing.T) {
	f := newFakeGH(t)
	h := &Host{Bin: f.bin}
	req := ports.OpenPullRequest{Head: "agent/x", Base: "main", Title: "T"}

	f.answer(`{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"A pull request already exists for acme:agent/x."}],"status":"422"}`,
		"gh: Validation Failed (HTTP 422)", 1)
	if _, err := h.Open(context.Background(), widgets, req); !errors.Is(err, ports.ErrPullRequestExists) {
		t.Fatalf("already exists = %v, want ErrPullRequestExists", err)
	}

	f.answer(`{"message":"Validation Failed","errors":[{"resource":"PullRequest","field":"base","code":"invalid"}],"status":"422"}`,
		"gh: Validation Failed (HTTP 422)", 1)
	_, err := h.Open(context.Background(), widgets, req)
	if errs.Code(err) != domain.CodePRCreateFailed || !strings.Contains(err.Error(), "Validation Failed") || !strings.Contains(err.Error(), "base") {
		t.Fatalf("validation = %v, want PR_CREATE_FAILED with the host's reason", err)
	}

	f.answer(`{"message":"Bad credentials","status":"401"}`, "gh: Bad credentials (HTTP 401)", 1)
	if _, err := h.Open(context.Background(), widgets, req); errs.Code(err) != domain.CodePRToolUnavailable {
		t.Fatalf("bad credentials = %v, want PR_TOOL_UNAVAILABLE", err)
	}
}

func TestFindOpenAsksForTheHeadsOpenPullRequest(t *testing.T) {
	f := newFakeGH(t)
	h := &Host{Bin: f.bin}
	f.answer("["+prJSON+"]", "", 0)
	got, err := h.FindOpen(context.Background(), widgets, "agent/x")
	if err != nil || got == nil || got.Number != 12 {
		t.Fatalf("found = %+v, %v", got, err)
	}
	args := strings.Join(f.args(), " ")
	for _, frag := range []string{"api --hostname github.com -X GET repos/acme/widgets/pulls", "-f head=acme:agent/x", "-f state=open"} {
		if !strings.Contains(args, frag) {
			t.Fatalf("args %q lack %q", args, frag)
		}
	}

	f.answer("[]", "", 0)
	if got, err := h.FindOpen(context.Background(), widgets, "agent/x"); err != nil || got != nil {
		t.Fatalf("none = %+v, %v", got, err)
	}
}

func TestGetMapsMergedAndClosed(t *testing.T) {
	f := newFakeGH(t)
	h := &Host{Bin: f.bin}
	f.answer(strings.Replace(strings.Replace(prJSON, `"state":"open"`, `"state":"closed"`, 1), `"merged_at":null`, `"merged_at":"2026-10-08T09:00:00Z"`, 1), "", 0)
	got, err := h.Get(context.Background(), widgets, 12)
	if err != nil || got.State != domain.PullRequestMerged {
		t.Fatalf("merged = %+v, %v", got, err)
	}
	if args := strings.Join(f.args(), " "); args != "api --hostname github.com repos/acme/widgets/pulls/12" {
		t.Fatalf("args = %q", args)
	}
	f.answer(strings.Replace(prJSON, `"state":"open"`, `"state":"closed"`, 1), "", 0)
	if got, _ := h.Get(context.Background(), widgets, 12); got.State != domain.PullRequestClosed {
		t.Fatalf("closed = %+v", got)
	}
}

func TestACanceledContextStopsGh(t *testing.T) {
	f := newFakeGH(t)
	h := &Host{Bin: f.bin}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Get(ctx, widgets, 1); err == nil {
		t.Fatal("a canceled call succeeded")
	}
}
