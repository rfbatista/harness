package vfs_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// countingIgnore counts the git calls a listing makes.
type countingIgnore struct {
	inner ports.GitIgnore
	calls int
}

func (c *countingIgnore) IgnoredPaths(ctx context.Context, dir string, paths []string) (map[string]bool, error) {
	c.calls++
	return c.inner.IgnoredPaths(ctx, dir, paths)
}

// browseWorld is session "s1" (a status of the test's choosing) on a real git
// worktree, with a resources root opened or not.
type browseWorld struct {
	*world
	sess   *domain.Session
	ignore *countingIgnore
	files  ports.SessionFiles
}

func newBrowseWorld(t *testing.T, status domain.SessionStatus) *browseWorld {
	t.Helper()
	worktree := t.TempDir()
	if out, err := exec.Command("git", "-C", worktree, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(worktree, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(worktree, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "node_modules/\n")
	write("node_modules/dep/index.js", "x")
	write("web/src/main.js", "export {}\n")
	write("web/src/util.js", "export {}\n")
	write("web/blob.bin", "a\x00b")
	write("README.md", "# hi\n")
	if err := os.Symlink("../README.md", filepath.Join(worktree, "web", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(worktree, "outdir")); err != nil {
		t.Fatal(err)
	}
	events := vfs.NewRootEvents()
	resources := vfs.New(vfs.Sources{Tickets: fakeTickets{}, Documents: fakeDocuments{}})
	resources.UseRootEvents(events)
	sess := &domain.Session{ID: "s1", WorkspaceID: "w1", Status: status}
	sessions := fakeSessions{list: []*domain.Session{sess, {ID: "bare"}}}
	workspaces := fakeWorkspaces{byID: map[string]*domain.Workspace{"w1": {ID: "w1", Path: worktree}}}
	fsys := vfs.NewFileSystem(resources, sessions, workspaces, filesystem.NewWorktreeFiles(), events)
	ignore := &countingIgnore{inner: gitcli.NewCode()}
	fsys.UseGitIgnore(ignore)
	return &browseWorld{
		world: &world{fs: fsys, resources: resources, worktree: worktree, events: events},
		sess:  sess, ignore: ignore, files: fsys,
	}
}

func TestRootsListTheWorktreeThenTheResourcesWithTheAgentState(t *testing.T) {
	for status, state := range map[domain.SessionStatus]string{
		domain.SessionThinking: ports.AgentWorking, domain.SessionRunning: ports.AgentWorking,
		domain.SessionWaitingApproval: ports.AgentWaiting, domain.SessionIdle: ports.AgentIdle,
		domain.SessionDone: ports.AgentEnded, domain.SessionFailed: ports.AgentEnded, domain.SessionStopped: ports.AgentEnded,
	} {
		w := newBrowseWorld(t, status)
		w.open(t, "s1")
		got, err := w.files.SessionRoots(context.Background(), "s1")
		if err != nil {
			t.Fatal(err)
		}
		if got.AgentState != state || got.SessionID != "s1" {
			t.Errorf("%s: state %q, want %q", status, got.AgentState, state)
		}
		if len(got.Roots) != 2 || got.Roots[0].Root != "worktree" || got.Roots[1].Root != "resources" {
			t.Fatalf("roots = %+v", got.Roots)
		}
		if !got.Roots[0].Available || got.Roots[0].Label != "Worktree" || !got.Roots[1].Available || got.Roots[1].Label != "Resources" {
			t.Errorf("%s: roots = %+v", status, got.Roots)
		}
	}
}

func TestAnEndedSessionKeepsItsWorktreeAndLosesItsResources(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionDone)
	got, _ := w.files.SessionRoots(context.Background(), "s1") // no resources root is open: the session ended
	if !got.Roots[0].Available || got.Roots[0].Note != "" {
		t.Errorf("worktree = %+v", got.Roots[0])
	}
	if got.Roots[1].Available || got.Roots[1].Note != "The resources folder ended with the session." {
		t.Errorf("resources = %+v", got.Roots[1])
	}
}

func TestARemovedWorktreeIsUnavailableWithItsNote(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionDone)
	if err := os.RemoveAll(w.worktree); err != nil {
		t.Fatal(err)
	}
	got, _ := w.files.SessionRoots(context.Background(), "s1")
	if got.Roots[0].Available || got.Roots[0].Note != "The worktree was removed." {
		t.Errorf("worktree = %+v", got.Roots[0])
	}
}

func TestASessionWithoutAWorkspaceHasNoFiles(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionIdle)
	if _, err := w.files.SessionRoots(context.Background(), "bare"); errs.Code(err) != "FILES_NOT_AVAILABLE" {
		t.Errorf("bare session: %v", err)
	}
	if _, err := w.files.SessionRoots(context.Background(), "nope"); errs.Code(err) != "SESSION_NOT_FOUND" {
		t.Errorf("unknown session: %v", err)
	}
}

func names(es []ports.SessionEntry) string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return strings.Join(out, ",")
}

func TestEntriesListFoldersFirstHideGitAndMarkIgnoredFolders(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionIdle)
	got, err := w.files.SessionEntries(context.Background(), "s1", ports.SessionEntriesQuery{Root: "worktree"})
	if err != nil {
		t.Fatal(err)
	}
	if names(got.Entries) != "node_modules,web,.gitignore,README.md,outdir" && names(got.Entries) != "node_modules,web,.gitignore,README.md" {
		// outdir is a symlink: it sorts with the files
		t.Fatalf("entries = %s", names(got.Entries))
	}
	for _, e := range got.Entries {
		if e.Name == ".git" {
			t.Fatal(".git is listed")
		}
		switch e.Name {
		case "node_modules":
			if e.Kind != "dir" || e.Ignored == nil || !*e.Ignored {
				t.Errorf("node_modules = %+v", e)
			}
		case "web":
			if e.Ignored == nil || *e.Ignored {
				t.Errorf("web = %+v", e)
			}
		case "README.md":
			if e.Kind != "file" || e.Size == nil || *e.Size != 5 {
				t.Errorf("README.md = %+v", e)
			}
		case "outdir":
			if e.Kind != "symlink" || e.Target == "" {
				t.Errorf("outdir = %+v", e)
			}
		}
	}
	if got.Next != "" {
		t.Errorf("next = %q on the only page", got.Next)
	}
	web, _ := w.files.SessionEntries(context.Background(), "s1", ports.SessionEntriesQuery{Root: "worktree", Dir: "web"})
	if names(web.Entries) != "src,blob.bin,link" || web.Entries[2].Target != "../README.md" {
		t.Errorf("web = %s %+v", names(web.Entries), web.Entries)
	}
}

func TestEntriesPageWithAfterAndLimit(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionIdle)
	dir := filepath.Join(w.worktree, "many")
	if err := os.MkdirAll(filepath.Join(dir, "d1"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	after := ""
	pages := 0
	for {
		page, err := w.files.SessionEntries(context.Background(), "s1", ports.SessionEntriesQuery{Root: "worktree", Dir: "many", After: after, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, e := range page.Entries {
			seen = append(seen, e.Name)
		}
		if page.Next == "" {
			break
		}
		if len(page.Entries) != 2 {
			t.Fatalf("a full page has %d entries", len(page.Entries))
		}
		after = page.Next
	}
	if strings.Join(seen, ",") != "d1,f0,f1,f2,f3,f4" || pages != 3 {
		t.Errorf("seen %v over %d pages", seen, pages)
	}
	if _, err := w.files.SessionEntries(context.Background(), "s1", ports.SessionEntriesQuery{Root: "worktree", Dir: "many", Limit: 5000}); err != nil {
		t.Errorf("a limit over 1000 is clamped, not refused: %v", err)
	}
}

func TestAnEntriesPageMakesAtMostOneGitCallWhateverItsSize(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionIdle)
	for _, n := range []int{10, 300} {
		dir := filepath.Join(w.worktree, fmt.Sprintf("big%d", n))
		for i := 0; i < n; i++ {
			if err := os.MkdirAll(filepath.Join(dir, fmt.Sprintf("d%04d", i)), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		w.ignore.calls = 0
		if _, err := w.files.SessionEntries(context.Background(), "s1", ports.SessionEntriesQuery{Root: "worktree", Dir: fmt.Sprintf("big%d", n)}); err != nil {
			t.Fatal(err)
		}
		if w.ignore.calls != 1 {
			t.Errorf("%d folders: %d git calls, want 1", n, w.ignore.calls)
		}
	}
}

func TestEntriesRefusals(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionDone)
	ctx := context.Background()
	for dir, code := range map[string]string{
		"..": "INVALID_INPUT", "web/../..": "INVALID_INPUT", "/etc": "INVALID_INPUT", "web/": "INVALID_INPUT",
		"nope": "PATH_NOT_FOUND", "outdir": "INVALID_INPUT", "web/link": "INVALID_INPUT", "web/link/x": "PATH_NOT_FOUND",
	} {
		if _, err := w.files.SessionEntries(ctx, "s1", ports.SessionEntriesQuery{Root: "worktree", Dir: dir}); errs.Code(err) != code {
			t.Errorf("dir %q: %v, want %s", dir, err, code)
		}
	}
	if _, err := w.files.SessionEntries(ctx, "s1", ports.SessionEntriesQuery{Root: "elsewhere"}); errs.Code(err) != "INVALID_INPUT" {
		t.Errorf("unknown root: %v", err)
	}
	_, err := w.files.SessionEntries(ctx, "s1", ports.SessionEntriesQuery{Root: "resources"})
	if errs.Code(err) != "ROOT_GONE" || !strings.Contains(err.Error(), "The resources folder ended with the session.") {
		t.Errorf("resources of an ended session: %v", err)
	}
}

func TestTheResourcesRootListsEverythingWithoutGit(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionIdle)
	w.open(t, "s1")
	w.ignore.calls = 0
	got, err := w.files.SessionEntries(context.Background(), "s1", ports.SessionEntriesQuery{Root: "resources"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(","+names(got.Entries)+",", ",README.md,") || !strings.Contains(names(got.Entries), ".index.json") {
		t.Errorf("resources = %s", names(got.Entries))
	}
	if w.ignore.calls != 0 {
		t.Errorf("the resources root made %d git calls", w.ignore.calls)
	}
	for _, e := range got.Entries {
		if e.Ignored != nil {
			t.Errorf("%s has an ignored flag in the resources root", e.Name)
		}
	}
}

func TestAFileReadsLikeTheCodeTabAndRefusesWhatEntriesRefuse(t *testing.T) {
	w := newBrowseWorld(t, domain.SessionIdle)
	w.open(t, "s1")
	ctx := context.Background()
	f, err := w.files.SessionFile(ctx, "s1", "worktree", "web/src/main.js")
	if err != nil || f.Content == nil || *f.Content != "export {}\n" || f.LineCount != 1 || f.Language == "" || f.Binary {
		t.Fatalf("main.js = %+v, %v", f, err)
	}
	if b, err := w.files.SessionFile(ctx, "s1", "worktree", "web/blob.bin"); err != nil || !b.Binary || b.Content != nil {
		t.Errorf("blob.bin = %+v, %v", b, err)
	}
	if r, err := w.files.SessionFile(ctx, "s1", "resources", "README.md"); err != nil || r.Content == nil {
		t.Errorf("resources README = %+v, %v", r, err)
	}
	big := strings.Repeat("x", domain.MaxCodeFileBytes+10)
	if err := os.WriteFile(filepath.Join(w.worktree, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := w.files.SessionFile(ctx, "s1", "worktree", "big.txt"); err != nil || !b.Truncated || b.Size != int64(len(big)) {
		t.Errorf("big.txt = %+v, %v", b, err)
	}
	for p, code := range map[string]string{"../x": "INVALID_INPUT", "/etc/passwd": "INVALID_INPUT", "": "PATH_NOT_FOUND", "missing": "PATH_NOT_FOUND", "outdir/x": "INVALID_INPUT", "web/link": "INVALID_INPUT", "web": "PATH_NOT_FOUND"} {
		if _, err := w.files.SessionFile(ctx, "s1", "worktree", p); errs.Code(err) != code {
			t.Errorf("file %q: %v, want %s", p, err, code)
		}
	}
	gone := newBrowseWorld(t, domain.SessionDone)
	if _, err := gone.files.SessionFile(ctx, "s1", "resources", "README.md"); errs.Code(err) != "ROOT_GONE" {
		t.Errorf("resources file of an ended session: %v", err)
	}
}
