//go:build !windows

package app

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The PATH shims, run as real processes the way an agent's Bash tool runs
// them: the server binary, built from cmd/server, linked under each
// command's name in the session's shim dir, with the session's env
// (contract "Harness server ↔ Agent sessions — virtual shell" §5–6).

// shimSession is a task session started with shims, and the env its
// processes run with.
type shimSession struct {
	runningSession
	env []string
}

func buildServer(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "server")
	out, err := exec.Command("go", "build", "-o", bin, "operators-mcp/cmd/server").CombinedOutput()
	if err != nil {
		t.Fatalf("build the server: %v\n%s", err, out)
	}
	return bin
}

func (e *shellE2E) startWithShims(ticketID, bin string) shimSession {
	e.t.Helper()
	e.orch.ShimBinary = bin
	e.orch.SessionShellURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/shell" }
	sess, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: ticketID, Mode: "implement"})
	if err != nil {
		e.t.Fatal(err)
	}
	shell := ""
	for _, kv := range spec.Env {
		if v, ok := strings.CutPrefix(kv, "HARNESS_SHELL="); ok {
			shell = v
		}
	}
	if shell == "" {
		e.t.Fatalf("session has no HARNESS_SHELL in %v", spec.Env)
	}
	return shimSession{runningSession{id: sess.ID, worktree: spec.Dir, shell: shell}, spec.Env}
}

// ran is what a command line printed and how it exited.
type ran struct {
	stdout, stderr string
	code           int
}

// sh runs line with /bin/sh in dir, with this process's env and then env,
// so the commands are found on PATH as an agent's would be.
func sh(t *testing.T, dir string, env []string, stdin, line string) ran {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", line)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("run %q: %v", line, err)
	}
	return ran{out.String(), errOut.String(), cmd.ProcessState.ExitCode()}
}

func (s shimSession) run(t *testing.T, stdin, line string) ran {
	t.Helper()
	return sh(t, s.shell, s.env, stdin, line)
}

func (s shimSession) ok(t *testing.T, stdin, line string) ran {
	t.Helper()
	r := s.run(t, stdin, line)
	if r.code != 0 {
		t.Fatalf("%q exited %d: %s", line, r.code, r.stderr)
	}
	return r
}

// inPlace is sed's in-place flag with no backup, as the real sed spells it.
func inPlace() string {
	if runtime.GOOS == "darwin" || strings.HasSuffix(runtime.GOOS, "bsd") {
		return "-i ''"
	}
	return "-i"
}

func TestShimsStoreWhatCommandsDoInTheShell(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	taskA := e.task("Shims")
	a := e.startWithShims(taskA, bin)

	doc := e.createDocument("Plan", "# Plan\n\ndraft\n", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	eventually(t, "the plan in the shell", func() error { return hasFile(a.shell, "task/docs/plan.md", "# Plan\n\ndraft\n", doc.ID, "task", "") })

	// sed -i: the edit is stored.
	a.ok(t, "", "sed "+inPlace()+" -e 's/draft/final/' task/docs/plan.md")
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, "# Plan\n\nfinal\n")

	// tee: a new document, with tee's output; tee -a appends.
	if r := a.ok(t, "# Notes\n", "tee task/docs/notes.md"); r.stdout != "# Notes\n" {
		t.Fatalf("tee printed %q", r.stdout)
	}
	notes := indexedID(t, a.runningSession, "task/docs/notes.md")
	a.ok(t, "more\n", "tee -a task/docs/notes.md >/dev/null")
	wantDocument(t, e.document(notes), "Notes", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, "# Notes\nmore\n")

	// mv to project/docs is refused and leaves the file in place; once the
	// person has moved it, mv within project/docs renames.
	if r := a.run(t, "", "mv task/docs/plan.md project/docs/"); r.code != 1 || !strings.HasPrefix(r.stderr, "PROJECT_MOVE_DENIED: ") {
		t.Fatalf("mv to project/docs = %+v, want exit 1 with PROJECT_MOVE_DENIED", r)
	}
	if err := hasFile(a.shell, "task/docs/plan.md", "# Plan\n\nfinal\n", doc.ID, "task", ""); err != nil {
		t.Fatal(err)
	}
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, "# Plan\n\nfinal\n")
	e.post("set_document_scope", map[string]string{"document_id": doc.ID, "scope": "project"}, nil)
	eventually(t, "the plan in project/docs", func() error {
		return hasFile(a.shell, "project/docs/plan.md", "# Plan\n\nfinal\n", doc.ID, "project", "")
	})
	a.ok(t, "", "mv -f project/docs/plan.md project/docs/the-plan.md")
	if err := hasFile(a.shell, "project/docs/the-plan.md", "# Plan\n\nfinal\n", doc.ID, "project", ""); err != nil {
		t.Fatal(err)
	}
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatMarkdown, domain.DocumentScopeProject, "# Plan\n\nfinal\n")

	// cp from outside: a new document.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "spec.md"), []byte("# Spec copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.ok(t, "", "cp "+filepath.Join(outside, "spec.md")+" task/docs/")
	wantDocument(t, e.document(indexedID(t, a.runningSession, "task/docs/spec.md")), "Spec copy", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, "# Spec copy\n")

	// mkdir -p under docs, then touch a file in it: a new document.
	a.ok(t, "", "mkdir -p task/docs/drafts/old")
	if info, err := os.Stat(filepath.Join(a.shell, "task", "docs", "drafts", "old")); err != nil || !info.IsDir() {
		t.Fatalf("mkdir -p made nothing: %v", err)
	}
	a.ok(t, "", "touch task/docs/drafts/empty.txt")
	wantDocument(t, e.document(indexedID(t, a.runningSession, "task/docs/drafts/empty.txt")), "empty", domain.DocumentFormatText, domain.DocumentScopeTask, "")

	// rm deletes the document.
	a.ok(t, "", "rm task/docs/notes.md")
	if d := e.document(notes); d != nil {
		t.Fatalf("the removed document is still stored: %+v", d)
	}
}

// Outside the shell, and for sed without -i, the shims print and exit
// exactly as the real commands do.
func TestShimsAreTransparentOutsideTheShell(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	taskA := e.task("Shims")
	a := e.startWithShims(taskA, bin)
	doc := e.createDocument("Plan", "# Plan\n\ndraft\n", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	eventually(t, "the plan in the shell", func() error { return hasFile(a.shell, "task/docs/plan.md", "# Plan\n\ndraft\n", doc.ID, "task", "") })

	plan := filepath.Join(a.shell, "task", "docs", "plan.md")
	const setup = "printf 'a\\nb\\n' > f; "
	lines := []string{
		setup + "sed -n 2p f && sed " + inPlace() + " 's/a/x/' f && cat f",
		setup + "sed 's/a/' f",
		"sed -n 1p no-such-file",
		setup + "printf 'q\\n' | tee g && tee -a g < f && cat g",
		setup + "cp f h && cp no-such-file h2; echo $?; cat h",
		"mkdir d && mkdir d; echo $?; mkdir -p d/e/f && touch d/e/f/t && ls -R d",
		setup + "mkdir d && mv f d/ && mv no-such-file d/; echo $?; ls d",
		setup + "mkdir -p d/e; rm no-such-file; echo $?; rm d; echo $?; rm -r d && rm -f f g && ls",
		"sed -n 3p " + plan,
		"sed 's/draft/final/' " + plan + " && cat " + plan,
	}
	real := []string{}
	for _, kv := range a.env {
		if !strings.HasPrefix(kv, "PATH=") {
			real = append(real, kv)
		}
	}
	for _, line := range lines {
		viaShims := sh(t, t.TempDir(), a.env, "", line)
		viaReal := sh(t, t.TempDir(), real, "", line)
		if viaShims != viaReal {
			t.Errorf("%s\nshims: %+v\nreal:  %+v", line, viaShims, viaReal)
		}
	}
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, "# Plan\n\ndraft\n")
}

// A refused operation exits 1 with the refusal on stderr, starting with its
// code, and changes nothing; with the harness down it is SHELL_UNAVAILABLE.
func TestShimsRefuseWithTheCodeAndWriteNothing(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	taskA := e.task("Shims")
	a := e.startWithShims(taskA, bin)
	taskJSON := filepath.Join(a.shell, "task", "task.json")
	before, err := os.ReadFile(taskJSON)
	if err != nil {
		t.Fatal(err)
	}

	refused := func(line, code string) {
		t.Helper()
		r := a.run(t, "x\n", line)
		if r.code != 1 || !strings.HasPrefix(r.stderr, code+": ") {
			t.Fatalf("%q = %+v, want exit 1 with %s on stderr", line, r, code)
		}
	}
	refused("sed "+inPlace()+" 's/Shims/Hacked/' task/task.json", "READ_ONLY")
	refused("tee task/task.json", "READ_ONLY")
	refused("rm -f task/task.json", "READ_ONLY")
	refused("mkdir task/resources/more", "READ_ONLY")
	if after, _ := os.ReadFile(taskJSON); !bytes.Equal(after, before) {
		t.Fatalf("task.json changed: %s", after)
	}
	if _, err := os.Stat(filepath.Join(a.shell, "task", "resources", "more")); err == nil {
		t.Fatal("the refused mkdir made its dir")
	}

	// The harness answers the check, then is gone before the commit: the
	// change is on disk, unstored, and the agent is told to redo it.
	lostCommit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/commit") {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		res, err := http.Post(e.srv.URL+r.URL.Path, "application/json", r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		defer res.Body.Close()
		_, _ = io.Copy(w, res.Body)
	}))
	defer lostCommit.Close()
	unstored := shimSession{a.runningSession, append(slices.Clone(a.env), "HARNESS_SHELL_URL="+lostCommit.URL+"/api/sessions/"+a.id+"/shell")}
	r := unstored.run(t, "# Unstored\n", "tee task/docs/unstored.md")
	want := "SHELL_UNAVAILABLE: the harness could not be reached; task/docs/unstored.md changed on disk but was not stored — redo the change once the harness is back\n"
	if r.code != 1 || r.stderr != want || r.stdout != "# Unstored\n" {
		t.Fatalf("tee with the commit lost = %+v, want exit 1 and %q", r, want)
	}

	e.srv.Close()
	refused("tee task/docs/lost.md", "SHELL_UNAVAILABLE")
	if r := a.run(t, "", "tee task/docs/lost.md"); r.stderr != "SHELL_UNAVAILABLE: the harness could not be reached; nothing was written\n" {
		t.Fatalf("stderr %q", r.stderr)
	}
	if _, err := os.Stat(filepath.Join(a.shell, "task", "docs", "lost.md")); err == nil {
		t.Fatal("the unchecked tee wrote its file")
	}
}
