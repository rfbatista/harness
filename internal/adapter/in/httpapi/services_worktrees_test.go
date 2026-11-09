package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/adapter/out/commandrunner"
	"operators-mcp/internal/adapter/out/containerrunner/containerrunnertest"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeRunWorktrees stands for the lookup the App tab uses (ADR 0039): the
// run worktrees of a repository, by what is on disk now.
type fakeRunWorktrees struct {
	mu   sync.Mutex
	list []*domain.RunWorktree
}

func (f *fakeRunWorktrees) ListRepositoryRunWorktrees(_ context.Context, _, _ string) ([]*domain.RunWorktree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.RunWorktree
	for _, w := range f.list {
		if fi, err := os.Stat(w.Dir); err == nil && fi.IsDir() {
			out = append(out, w)
		}
	}
	return out, nil
}

type mainBranch string

func (b mainBranch) CurrentBranch(context.Context, string) (string, error) { return string(b), nil }

// worktreesFixture is a repository whose main checkout and one session's
// worktree both hold a folder "cmd".
type worktreesFixture struct {
	*servicesFixture
	wt      string
	lookup  *fakeRunWorktrees
	session *domain.RunWorktree
}

func newWorktreesFixture(t *testing.T) *worktreesFixture {
	t.Helper()
	lookup := &fakeRunWorktrees{}
	f := newServicesFixture(t, services.Options{
		RunWorktrees: lookup,
		Branches:     mainBranch("main"),
		Runners: map[domain.ServiceKind]ports.ServiceProcessRunner{
			domain.ServiceKindCommand: commandrunner.Runner{}, domain.ServiceKindImage: &containerrunnertest.Fake{},
		},
	})
	wt := t.TempDir()
	for _, dir := range []string{f.repoDir(), wt} {
		if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	session := &domain.RunWorktree{
		Key: "session:s1", Kind: domain.RunWorktreeSession, Branch: "agent/feature", Dir: wt,
		SessionID: "s1", Agent: "go-developer", SessionStatus: domain.SessionRunning,
	}
	lookup.list = []*domain.RunWorktree{
		{Key: "main", Kind: domain.RunWorktreeMain, Branch: "main", Dir: f.repoDir()},
		session,
	}
	return &worktreesFixture{servicesFixture: f, wt: wt, lookup: lookup, session: session}
}

func (f *worktreesFixture) start(id, worktree string) (int, map[string]any) {
	f.t.Helper()
	return f.call("POST", "/api/start_service", map[string]any{"service_id": id, "worktree": worktree})
}

func TestServicesWorktrees_ListMainThenEachSessionWorktree(t *testing.T) {
	f := newWorktreesFixture(t)
	id := f.command("web", "true")

	code, out := f.call("GET", "/api/list_service_run_worktrees?service_id="+id, nil)
	if code != 200 {
		t.Fatalf("list = %d %v", code, out)
	}
	list := out["worktrees"].([]any)
	if len(list) != 2 {
		t.Fatalf("worktrees = %v", list)
	}
	main, sess := list[0].(map[string]any), list[1].(map[string]any)
	if main["key"] != "main" || main["kind"] != "main" || main["branch"] != "main" || main["dir"] != f.repoDir() {
		t.Fatalf("main first: %v", main)
	}
	if sess["key"] != "session:s1" || sess["kind"] != "session" || sess["branch"] != "agent/feature" || sess["agent"] != "go-developer" ||
		sess["session_id"] != "s1" || sess["session_status"] != "running" {
		t.Fatalf("session worktree: %v", sess)
	}
	if _, own := main["own"]; own {
		t.Fatalf("a service run worktree has no own key: %v", main)
	}

	// A worktree gone from disk is not offered.
	if err := os.RemoveAll(f.wt); err != nil {
		t.Fatal(err)
	}
	_, out = f.call("GET", "/api/list_service_run_worktrees?service_id="+id, nil)
	if got := out["worktrees"].([]any); len(got) != 1 {
		t.Fatalf("worktrees after removal = %v", got)
	}
}

func TestServicesWorktrees_ImageAndRepositoryLessServicesHaveNone(t *testing.T) {
	f := newWorktreesFixture(t)
	loose := commandInput("loose")
	imageID := f.createID(imageInput("db"))
	looseID := f.createID(loose)
	for _, id := range []string{imageID, looseID} {
		code, out := f.call("GET", "/api/list_service_run_worktrees?service_id="+id, nil)
		list, ok := out["worktrees"].([]any)
		if code != 200 || !ok || len(list) != 0 {
			t.Fatalf("list = %d %v, want an empty list", code, out)
		}
	}
	if code, out := f.call("GET", "/api/list_service_run_worktrees?service_id=nope", nil); code != 404 {
		t.Fatalf("an unknown service = %d %v", code, out)
	}

	for _, id := range []string{imageID, looseID} {
		code, out := f.start(id, "main")
		if c, _ := errorOf(out); code != 400 || c != "INVALID_INPUT" {
			t.Fatalf("worktree on a service with nothing to pick = %d %v", code, out)
		}
	}
}

func TestServicesWorktrees_AStartRunsInThePickedWorktreeAndRecordsIt(t *testing.T) {
	f := newWorktreesFixture(t)
	in := commandInput("web")
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": "echo ready; exec sleep 60", "subdir": "cmd"}
	id := f.createID(in)

	code, out := f.start(id, "session:s1")
	if code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	run := out["service"].(map[string]any)["run"].(map[string]any)
	where := run["where"].(map[string]any)
	// The subfolder applies inside the picked worktree.
	dir := filepath.Join(f.wt, "cmd")
	if where["kind"] != "session" || where["dir"] != dir || where["branch"] != "agent/feature" || where["session_id"] != "s1" || run["worktree_key"] != "session:s1" {
		t.Fatalf("run = %v", run)
	}
	if text := f.awaitLog(id, "\nready"); !strings.Contains(text, "[harness] directory: "+dir) {
		t.Fatalf("log = %q", text)
	}
	f.operate("stop_service", id)

	// No worktree, or an empty one, is the main checkout.
	for _, w := range []string{"", "main"} {
		_, out = f.start(id, w)
		run = out["service"].(map[string]any)["run"].(map[string]any)
		where = run["where"].(map[string]any)
		if run["worktree_key"] != "main" || where["kind"] != "main" || where["branch"] != "main" || where["dir"] != filepath.Join(f.repoDir(), "cmd") {
			t.Fatalf("start with worktree %q = %v", w, run)
		}
		f.operate("stop_service", id)
	}
}

func TestServicesWorktrees_AKeyThatIsNoLongerListedIsRefusedAndNothingStarts(t *testing.T) {
	f := newWorktreesFixture(t)
	id := f.command("web", "echo hello; exec sleep 60")
	f.start(id, "main")
	f.awaitLog(id, "hello")
	f.operate("stop_service", id)
	logBefore := f.logText(id)
	runBefore := f.service(id)["run"].(map[string]any)["id"]

	for _, key := range []string{"session:gone", "own", "/etc", "session:s1/../x"} {
		code, out := f.start(id, key)
		c, msg := errorOf(out)
		if code != 409 || c != "WORKTREE_NOT_AVAILABLE" || msg != "that worktree is no longer available" {
			t.Fatalf("start in %q = %d %v", key, code, out)
		}
	}
	if err := os.RemoveAll(f.wt); err != nil {
		t.Fatal(err)
	}
	if code, out := f.start(id, "session:s1"); code != 409 {
		t.Fatalf("a worktree removed since the page listed it = %d %v", code, out)
	}
	if got := f.service(id); got["status"] != "stopped" || got["run"].(map[string]any)["id"] != runBefore || f.logText(id) != logBefore {
		t.Fatalf("a refused start leaves the service and its log as they were: %v", got)
	}
}

func TestServicesWorktrees_RestartRepeatsTheLastWorktree(t *testing.T) {
	f := newWorktreesFixture(t)
	id := f.command("web", "echo pid=$$; exec sleep 60")
	f.start(id, "session:s1")
	f.awaitLog(id, "pid=")

	code, out := f.operate("restart_service", id)
	run := out["service"].(map[string]any)["run"].(map[string]any)
	if code != 200 || run["worktree_key"] != "session:s1" || run["where"].(map[string]any)["dir"] != f.wt {
		t.Fatalf("restart = %d %v", code, run)
	}

	// The recorded worktree is gone: the restart is refused and the run stays.
	runID := run["id"]
	if err := os.RemoveAll(f.wt); err != nil {
		t.Fatal(err)
	}
	code, out = f.operate("restart_service", id)
	if c, _ := errorOf(out); code != 409 || c != "WORKTREE_NOT_AVAILABLE" {
		t.Fatalf("restart with the worktree gone = %d %v", code, out)
	}
	if got := f.service(id); got["status"] != "running" || got["run"].(map[string]any)["id"] != runID {
		t.Fatalf("a refused restart leaves the service running: %v", got)
	}

	// Stop then Start begins from the definition: the main checkout.
	f.operate("stop_service", id)
	_, out = f.start(id, "")
	if got := out["service"].(map[string]any)["run"].(map[string]any)["worktree_key"]; got != "main" {
		t.Fatalf("a fresh start picks the main checkout, got %v", got)
	}
	f.operate("stop_service", id)
}

func TestServicesWorktrees_ASubfolderMissingFromThePickedWorktreeNamesBothSeparately(t *testing.T) {
	f := newWorktreesFixture(t)
	if err := os.RemoveAll(filepath.Join(f.wt, "cmd")); err != nil {
		t.Fatal(err)
	}
	in := commandInput("web")
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": "true", "subdir": "cmd"}
	id := f.createID(in)

	code, out := f.start(id, "session:s1")
	if code != 200 {
		t.Fatalf("a failure after the run was created is not a refusal: %d %v", code, out)
	}
	svc := out["service"].(map[string]any)
	run := svc["run"].(map[string]any)
	if svc["status"] != "failed" || run["error"] != `subfolder "cmd" does not exist in `+f.wt {
		t.Fatalf("service = %v", svc)
	}
}
