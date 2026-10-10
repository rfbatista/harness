package portstest

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// CodeTarget is where the runner recorded the fixture: its repository (in
// the fixture's project, at the fixture's RepoDir) and two workspaces, one
// at Worktree on Branch and one at GoneWorktree on GoneBranch, both with
// BaseRef.
type CodeTarget struct {
	RepositoryID, WorkspaceID, GoneWorkspaceID string
}

// CodeConformance runs the code browsing contract against the port newPort
// builds over the fixture.
func CodeConformance(t *testing.T, newPort func(t *testing.T, f *CodeFixture) (ports.CodeBrowser, CodeTarget)) {
	ctx := context.Background()
	f := NewCodeFixture(t)
	p, at := newPort(t, f)
	repo := ports.CodeSourceQuery{RepositoryID: at.RepositoryID}
	ws := ports.CodeSourceQuery{WorkspaceID: at.WorkspaceID}
	gone := ports.CodeSourceQuery{WorkspaceID: at.GoneWorkspaceID}
	refAt := func(ref string) ports.CodeSourceQuery {
		return ports.CodeSourceQuery{RepositoryID: at.RepositoryID, Ref: ref}
	}

	t.Run("a source is exactly one of a repository or a workspace", func(t *testing.T) {
		for name, q := range map[string]ports.CodeSourceQuery{
			"neither":           {},
			"both":              {RepositoryID: at.RepositoryID, WorkspaceID: at.WorkspaceID},
			"ref on workspace":  {WorkspaceID: at.WorkspaceID, Ref: "main"},
			"option-like ref":   refAt("--output=/tmp/x"),
			"revision expr":     refAt("main~1"),
			"reflog expression": refAt("main@{1}"),
		} {
			_, err := p.CodeTree(ctx, q, "")
			if !hasCode(err, "INVALID_INPUT") {
				t.Errorf("%s: err = %v, want INVALID_INPUT", name, err)
			}
		}
		_, err := p.CodeTree(ctx, ports.CodeSourceQuery{RepositoryID: "missing"}, "")
		wantCode(t, err, "REPOSITORY_NOT_FOUND")
		_, err = p.CodeTree(ctx, ports.CodeSourceQuery{WorkspaceID: "missing"}, "")
		wantCode(t, err, "WORKSPACE_NOT_FOUND")
		_, err = p.CodeFile(ctx, refAt("no-such-branch"), "README.md")
		wantCode(t, err, "REF_NOT_FOUND")
	})

	t.Run("a repository reads its checked-out branch by default", func(t *testing.T) {
		tree, err := p.CodeTree(ctx, repo, "")
		if err != nil {
			t.Fatal(err)
		}
		want := domain.CodeSource{Kind: domain.CodeSourceRef, RepositoryID: at.RepositoryID, Ref: "main", Commit: f.Merge}
		if !reflect.DeepEqual(tree.Source, want) {
			t.Fatalf("source = %+v, want %+v", tree.Source, want)
		}
		if tree.Path != "" {
			t.Fatalf("path = %q, want the root", tree.Path)
		}
		wantNames(t, tree.Entries, []string{"bulk", "docs", "internal", "web", ".gitignore", "README.md", "big.txt",
			"link-in", "link-out", "linkdir", "logo.png", "nonl.txt", "side.txt"})
		byName := entries(tree.Entries)
		if e := byName["internal"]; e.Kind != domain.EntryDir || e.Path != "internal" || e.Size != nil || !slices.Equal(e.Zones, []string{"zone-adapters"}) {
			t.Errorf("internal = %s", show(e))
		}
		if e := byName["web"]; !slices.Equal(e.Zones, []string{"zone-web"}) {
			t.Errorf("web zones = %v, want [zone-web]", e.Zones)
		}
		if e := byName["README.md"]; e.Kind != domain.EntryFile || e.Size == nil || *e.Size != 6 || e.Target != nil || e.Change != nil || e.Zones == nil || len(e.Zones) != 0 {
			t.Errorf("README.md = %s", show(e))
		}
		if e := byName["link-in"]; e.Kind != domain.EntrySymlink || e.Target == nil || *e.Target != "README.md" || e.Size != nil {
			t.Errorf("link-in = %s", show(e))
		}
		if e := byName["link-out"]; e.Kind != domain.EntrySymlink || e.Target == nil || *e.Target != "../../outside" {
			t.Errorf("link-out = %s", show(e))
		}

		sub, err := p.CodeTree(ctx, repo, "internal/app")
		if err != nil {
			t.Fatal(err)
		}
		wantNames(t, sub.Entries, []string{"app.go"})
		if e := sub.Entries[0]; e.Path != "internal/app/app.go" || !slices.Equal(e.Zones, []string{"zone-adapters"}) {
			t.Errorf("app.go = %s", show(e))
		}
	})

	t.Run("a ref or a hash reads that commit", func(t *testing.T) {
		tree, err := p.CodeTree(ctx, refAt(f.Root), "docs")
		if err != nil {
			t.Fatal(err)
		}
		if tree.Source.Ref != f.Root || tree.Source.Commit != f.Root {
			t.Errorf("source = %+v, want ref and commit %s", tree.Source, f.Root)
		}
		wantNames(t, tree.Entries, []string{"old.md"})
		short, err := p.CodeTree(ctx, refAt(f.Root[:10]), "")
		if err != nil {
			t.Fatal(err)
		}
		if short.Source.Commit != f.Root {
			t.Errorf("abbreviated hash resolved to %s, want %s", short.Source.Commit, f.Root)
		}
		side, err := p.CodeFile(ctx, refAt("side"), "side.txt")
		if err != nil {
			t.Fatal(err)
		}
		if side.Source.Commit != f.SideTip {
			t.Errorf("side resolved to %s, want %s", side.Source.Commit, f.SideTip)
		}
	})

	t.Run("unsafe and missing paths", func(t *testing.T) {
		for _, src := range []ports.CodeSourceQuery{repo, ws} {
			for _, bad := range []string{"../x", "/etc/passwd", ".git", ".git/config", "a/../../x", "internal\x00"} {
				_, err := p.CodeTree(ctx, src, bad)
				if !hasCode(err, "INVALID_INPUT") {
					t.Errorf("%s tree %q: err = %v, want INVALID_INPUT", kindOf(src), bad, err)
				}
				_, err = p.CodeFile(ctx, src, bad)
				if !hasCode(err, "INVALID_INPUT") {
					t.Errorf("%s file %q: err = %v, want INVALID_INPUT", kindOf(src), bad, err)
				}
			}
			for _, path := range []string{"link-out", "link-out/x"} {
				_, err := p.CodeFile(ctx, src, path)
				if !hasCode(err, "INVALID_INPUT") {
					t.Errorf("%s file %q: err = %v, want INVALID_INPUT (a link out of the root)", kindOf(src), path, err)
				}
				_, err = p.CodeTree(ctx, src, path)
				if !hasCode(err, "INVALID_INPUT") {
					t.Errorf("%s tree %q: err = %v, want INVALID_INPUT (a link out of the root)", kindOf(src), path, err)
				}
			}
			for _, c := range []struct {
				tree bool
				path string
			}{
				{true, "README.md"}, {true, "nope"}, {true, "linkdir"}, {true, "link-in"},
				{false, "internal"}, {false, "nope.go"}, {false, "link-in"}, {false, "linkdir/app/app.go"},
			} {
				var err error
				if c.tree {
					_, err = p.CodeTree(ctx, src, c.path)
				} else {
					_, err = p.CodeFile(ctx, src, c.path)
				}
				if !hasCode(err, "PATH_NOT_FOUND") {
					t.Errorf("%s tree=%v %q: err = %v, want PATH_NOT_FOUND", kindOf(src), c.tree, c.path, err)
				}
			}
		}
	})

	t.Run("every file path of a ref", func(t *testing.T) {
		got, err := p.CodePaths(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{".gitignore", "README.md", "big.txt", "bulk/a.txt", "bulk/b.txt", "bulk/c.txt", "bulk/d.txt",
			"bulk/e.txt", "bulk/f.txt", "bulk/g.txt", "docs/new.md", "internal/app/app.go", "link-in", "link-out",
			"linkdir", "logo.png", "nonl.txt", "side.txt", "web/src/main.js"}
		if !slices.Equal(got.Paths, want) || got.Truncated || got.Source.Commit != f.Merge {
			t.Fatalf("paths = %v (truncated %v, commit %s), want %v", got.Paths, got.Truncated, got.Source.Commit, want)
		}
	})

	t.Run("a file's content at a ref", func(t *testing.T) {
		got, err := p.CodeFile(ctx, repo, "README.md")
		if err != nil {
			t.Fatal(err)
		}
		if got.Path != "README.md" || got.Size != 6 || got.Binary || got.Truncated || got.Language != "markdown" ||
			got.Content == nil || *got.Content != "hello\n" || got.LineCount != 1 || got.Zones == nil || len(got.Zones) != 0 {
			t.Fatalf("README.md = %s", show(got))
		}
		app, err := p.CodeFile(ctx, repo, "internal/app/app.go")
		if err != nil {
			t.Fatal(err)
		}
		if app.Language != "go" || app.LineCount != 4 || !slices.Equal(app.Zones, []string{"zone-adapters"}) {
			t.Errorf("app.go = %s", show(app))
		}
		nonl, err := p.CodeFile(ctx, repo, "nonl.txt")
		if err != nil {
			t.Fatal(err)
		}
		if *nonl.Content != "first\nlast" || nonl.LineCount != 2 {
			t.Errorf("nonl.txt = %s", show(nonl))
		}
		bin, err := p.CodeFile(ctx, repo, "logo.png")
		if err != nil {
			t.Fatal(err)
		}
		if !bin.Binary || bin.Content != nil || bin.LineCount != 0 || bin.Size == 0 {
			t.Errorf("logo.png = %s", show(bin))
		}
		big, err := p.CodeFile(ctx, repo, "big.txt")
		if err != nil {
			t.Fatal(err)
		}
		if !big.Truncated || big.Size <= domain.MaxCodeFileBytes || big.Content == nil ||
			len(*big.Content) > domain.MaxCodeFileBytes || !strings.HasSuffix(*big.Content, "of the big file\n") ||
			big.LineCount != strings.Count(*big.Content, "\n") {
			t.Errorf("big.txt: truncated %v, size %d, %d bytes of content, %d lines", big.Truncated, big.Size, contentLen(big), big.LineCount)
		}
	})

	t.Run("a workspace reads its worktree on disk", func(t *testing.T) {
		tree, err := p.CodeTree(ctx, ws, "")
		if err != nil {
			t.Fatal(err)
		}
		id := at.WorkspaceID
		want := domain.CodeSource{Kind: domain.CodeSourceWorkspace, RepositoryID: at.RepositoryID, WorkspaceID: &id, Ref: f.Branch, Commit: f.SessionHead}
		if !reflect.DeepEqual(tree.Source, want) {
			t.Fatalf("source = %s, want %s", show(tree.Source), show(want))
		}
		wantNames(t, tree.Entries, []string{"bulk", "docs", "internal", "web", ".gitignore", "README.md", "big.txt",
			"link-in", "link-out", "linkdir", "logo.png", "new.txt", "nonl.txt", "side.txt"})
		changes := map[string]string{}
		for _, e := range tree.Entries {
			if e.Change != nil {
				changes[e.Name] = *e.Change
			}
		}
		wantChanges := map[string]string{"internal": "modified", "web": "modified", "README.md": "modified", "new.txt": "untracked", "nonl.txt": "deleted"}
		if !reflect.DeepEqual(changes, wantChanges) {
			t.Errorf("changes = %v, want %v", changes, wantChanges)
		}
		byName := entries(tree.Entries)
		if e := byName["nonl.txt"]; e.Kind != domain.EntryFile || e.Size != nil {
			t.Errorf("deleted nonl.txt = %s", show(e))
		}
		if e := byName["README.md"]; e.Size == nil || *e.Size != 12 {
			t.Errorf("README.md on disk = %s", show(e))
		}
		if e := byName["link-in"]; e.Kind != domain.EntrySymlink || e.Target == nil || *e.Target != "README.md" {
			t.Errorf("link-in = %s", show(e))
		}

		app, err := p.CodeTree(ctx, ws, "internal/app")
		if err != nil {
			t.Fatal(err)
		}
		wantNames(t, app.Entries, []string{"app.go", "feature.go"})
		if e := entries(app.Entries)["feature.go"]; e.Change == nil || *e.Change != "added" {
			t.Errorf("feature.go = %s, want change added", show(e))
		}
		if e := entries(app.Entries)["app.go"]; e.Change != nil {
			t.Errorf("app.go = %s, want no change", show(e))
		}

		paths, err := p.CodePaths(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(paths.Paths, "new.txt") || !slices.Contains(paths.Paths, "internal/app/feature.go") ||
			slices.Contains(paths.Paths, "nonl.txt") || slices.Contains(paths.Paths, "ignored.log") || !slices.IsSorted(paths.Paths) {
			t.Errorf("workspace paths = %v", paths.Paths)
		}

		readme, err := p.CodeFile(ctx, ws, "README.md")
		if err != nil {
			t.Fatal(err)
		}
		if *readme.Content != "hello\nworld\n" || readme.LineCount != 2 || readme.Source.Kind != domain.CodeSourceWorkspace {
			t.Errorf("README.md on disk = %s", show(readme))
		}
		_, err = p.CodeFile(ctx, ws, "nonl.txt")
		wantCode(t, err, "PATH_NOT_FOUND")
	})

	t.Run("one commit's patch against its first parent", func(t *testing.T) {
		d, err := p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: at.RepositoryID, Commit: f.Rename})
		if err != nil {
			t.Fatal(err)
		}
		if d.Scope != domain.ScopeCommit || d.Base != f.Root || d.Source.Commit != f.Rename || d.Source.Kind != domain.CodeSourceRef || d.Truncated {
			t.Fatalf("diff = scope %s base %s source %+v truncated %v", d.Scope, d.Base, d.Source, d.Truncated)
		}
		wantDiffPaths(t, d, []string{"docs/new.md", "internal/app/app.go"})
		guide := d.Files[0]
		if guide.Status != domain.ChangeRenamed || guide.OldPath == nil || *guide.OldPath != "docs/old.md" || guide.Added != 1 || guide.Deleted != 0 || guide.Language != "markdown" {
			t.Errorf("docs/new.md = %s", show(guide))
		}
		app := d.Files[1]
		if app.Status != domain.ChangeModified || app.Added != 1 || app.Deleted != 0 || app.Withheld != nil || len(app.Hunks) != 1 || !slices.Equal(app.Zones, []string{"zone-adapters"}) {
			t.Fatalf("app.go = %s", show(app))
		}
		h := app.Hunks[0]
		if h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 4 || len(h.Lines) != 4 {
			t.Fatalf("hunk = %s", show(h))
		}
		add := h.Lines[2]
		if add.Kind != domain.LineAdd || add.Text != "// Run runs." || add.Old != nil || add.New == nil || *add.New != 3 {
			t.Errorf("added line = %s", show(add))
		}
		ctxLine := h.Lines[3]
		if ctxLine.Kind != domain.LineContext || ctxLine.Old == nil || *ctxLine.Old != 3 || *ctxLine.New != 4 || ctxLine.Text != "func Run() {}" {
			t.Errorf("context line = %s", show(ctxLine))
		}
		if d.Added != 2 || d.Deleted != 0 {
			t.Errorf("totals = +%d -%d, want +2 -0", d.Added, d.Deleted)
		}
	})

	t.Run("a merge commit against its first parent", func(t *testing.T) {
		d, err := p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: at.RepositoryID, Commit: f.Merge})
		if err != nil {
			t.Fatal(err)
		}
		if d.Base != f.Rename {
			t.Errorf("base = %s, want the first parent %s", d.Base, f.Rename)
		}
		wantDiffPaths(t, d, []string{"side.txt"})
	})

	t.Run("a root commit against the empty tree, within the limits", func(t *testing.T) {
		d, err := p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: at.RepositoryID, Commit: f.Root})
		if err != nil {
			t.Fatal(err)
		}
		if d.Base != f.EmptyTree {
			t.Errorf("base = %s, want the empty tree %s", d.Base, f.EmptyTree)
		}
		files := map[string]domain.FileDiff{}
		for _, fd := range d.Files {
			files[fd.Path] = fd
			if fd.Status != domain.ChangeAdded {
				t.Errorf("%s status = %s, want added", fd.Path, fd.Status)
			}
		}
		if fd := files["logo.png"]; !fd.Binary || fd.Added != -1 || fd.Deleted != -1 || fd.Hunks != nil || !withheld(fd, domain.WithheldBinary) {
			t.Errorf("logo.png = %s", show(fd))
		}
		if fd := files["big.txt"]; fd.Added != fixtureBigLines || fd.Hunks != nil || !withheld(fd, domain.WithheldTooLarge) {
			t.Errorf("big.txt: added %d, %d hunks, withheld %v", fd.Added, len(fd.Hunks), fd.Withheld)
		}
		// bulk/a..f fit under the total; bulk/g passes it.
		for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
			if fd := files["bulk/"+name+".txt"]; fd.Withheld != nil || len(fd.Hunks) != 1 || len(fd.Hunks[0].Lines) != fixtureBulkLines {
				t.Errorf("bulk/%s.txt: withheld %v, %d hunks", name, fd.Withheld, len(fd.Hunks))
			}
		}
		if fd := files["bulk/g.txt"]; !withheld(fd, domain.WithheldTooLarge) || fd.Hunks != nil {
			t.Errorf("bulk/g.txt: withheld %v, want too_large", fd.Withheld)
		}
		if !d.Truncated {
			t.Error("truncated = false, want true: the total withheld bulk/g.txt")
		}
		if fd := files["nonl.txt"]; len(fd.Hunks) != 1 || !fd.Hunks[0].Lines[1].NoNewline || fd.Hunks[0].Lines[0].NoNewline {
			t.Errorf("nonl.txt = %s", show(fd))
		}
		if fd := files["link-in"]; fd.Withheld != nil || len(fd.Hunks) != 1 || fd.Hunks[0].Lines[0].Text != "README.md" {
			t.Errorf("link-in = %s", show(fd))
		}
		wantAdded := 0
		for _, fd := range d.Files {
			if fd.Added > 0 {
				wantAdded += fd.Added
			}
		}
		if d.Added != wantAdded || d.Deleted != 0 {
			t.Errorf("totals = +%d -%d, want +%d -0 (binary excluded)", d.Added, d.Deleted, wantAdded)
		}

		one, err := p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: at.RepositoryID, Commit: f.Root, Path: "bulk/g.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if len(one.Files) != 1 || one.Files[0].Withheld != nil || len(one.Files[0].Hunks) != 1 {
			t.Errorf("bulk/g.txt alone = %d files, withheld %v", len(one.Files), one.Files[0].Withheld)
		}
		huge, err := p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: at.RepositoryID, Commit: f.Root, Path: "big.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if len(huge.Files) != 1 || !withheld(huge.Files[0], domain.WithheldTooLarge) || huge.Files[0].Added != fixtureBigLines {
			t.Errorf("big.txt alone = %s", show(huge.Files))
		}
	})

	t.Run("a range from the merge base", func(t *testing.T) {
		d, err := p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: at.RepositoryID, Base: "main", Head: f.Branch})
		if err != nil {
			t.Fatal(err)
		}
		if d.Scope != domain.ScopeRange || d.Base != f.Merge || d.Source.Ref != f.Branch || d.Source.Commit != f.SessionHead {
			t.Errorf("diff = scope %s base %s source %+v", d.Scope, d.Base, d.Source)
		}
		wantDiffPaths(t, d, []string{"internal/app/feature.go", "web/src/main.js"})
	})

	t.Run("a workspace's diff scopes", func(t *testing.T) {
		all, err := p.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: at.WorkspaceID})
		if err != nil {
			t.Fatal(err)
		}
		if all.Scope != domain.ScopeAll || all.Base != f.Merge || all.Source.Kind != domain.CodeSourceWorkspace || all.Source.Commit != f.SessionHead {
			t.Errorf("all = scope %s base %s source %+v", all.Scope, all.Base, all.Source)
		}
		wantDiffPaths(t, all, []string{"README.md", "internal/app/feature.go", "new.txt", "nonl.txt", "web/src/main.js"})
		statuses := map[string]string{}
		for _, fd := range all.Files {
			statuses[fd.Path] = fd.Status
		}
		wantStatuses := map[string]string{"README.md": "modified", "internal/app/feature.go": "added", "new.txt": "untracked", "nonl.txt": "deleted", "web/src/main.js": "modified"}
		if !reflect.DeepEqual(statuses, wantStatuses) {
			t.Errorf("statuses = %v, want %v", statuses, wantStatuses)
		}
		for _, fd := range all.Files {
			if fd.Path != "new.txt" {
				continue
			}
			if fd.Added != 2 || fd.Deleted != 0 || len(fd.Hunks) != 1 {
				t.Fatalf("new.txt = %s", show(fd))
			}
			h := fd.Hunks[0]
			if h.OldStart != 0 || h.OldLines != 0 || h.NewStart != 1 || h.NewLines != 2 || h.Lines[1].Text != "new" || *h.Lines[1].New != 2 || h.Lines[1].Kind != domain.LineAdd {
				t.Errorf("new.txt hunk = %s", show(h))
			}
		}

		branch, err := p.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: at.WorkspaceID, Scope: "branch"})
		if err != nil {
			t.Fatal(err)
		}
		if branch.Scope != domain.ScopeBranch || branch.Base != f.Merge || branch.Source.Gone {
			t.Errorf("branch = scope %s base %s gone %v", branch.Scope, branch.Base, branch.Source.Gone)
		}
		wantDiffPaths(t, branch, []string{"internal/app/feature.go", "web/src/main.js"})

		working, err := p.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: at.WorkspaceID, Scope: "working"})
		if err != nil {
			t.Fatal(err)
		}
		if working.Scope != domain.ScopeWorking || working.Base != f.SessionHead {
			t.Errorf("working = scope %s base %s", working.Scope, working.Base)
		}
		wantDiffPaths(t, working, []string{"README.md", "new.txt", "nonl.txt"})
		for _, fd := range working.Files {
			if fd.Path == "nonl.txt" {
				lines := fd.Hunks[0].Lines
				if fd.Deleted != 2 || len(lines) != 2 || lines[1].Kind != domain.LineDel || !lines[1].NoNewline || lines[1].New != nil || *lines[1].Old != 2 {
					t.Errorf("deleted nonl.txt = %s", show(fd))
				}
			}
		}

		one, err := p.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: at.WorkspaceID, Path: "README.md"})
		if err != nil {
			t.Fatal(err)
		}
		wantDiffPaths(t, one, []string{"README.md"})
		if one.Added != 1 || one.Deleted != 0 {
			t.Errorf("README.md alone totals = +%d -%d", one.Added, one.Deleted)
		}
		_, err = p.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: at.WorkspaceID, Path: "docs/new.md"})
		wantCode(t, err, "PATH_NOT_FOUND")
	})

	t.Run("bad diff sources", func(t *testing.T) {
		for name, q := range map[string]ports.CodeDiffQuery{
			"nothing":           {},
			"repository alone":  {RepositoryID: at.RepositoryID},
			"commit and range":  {RepositoryID: at.RepositoryID, Commit: f.Root, Base: "main", Head: "side"},
			"base without head": {RepositoryID: at.RepositoryID, Base: "main"},
			"unknown scope":     {WorkspaceID: at.WorkspaceID, Scope: "everything"},
			"scope on a commit": {RepositoryID: at.RepositoryID, Commit: f.Root, Scope: "all"},
			"not a hash":        {RepositoryID: at.RepositoryID, Commit: "main"},
			"option-like base":  {RepositoryID: at.RepositoryID, Base: "-p", Head: "main"},
			"workspace + repo":  {WorkspaceID: at.WorkspaceID, RepositoryID: at.RepositoryID, Commit: f.Root},
			"unsafe path":       {RepositoryID: at.RepositoryID, Commit: f.Root, Path: "../x"},
		} {
			_, err := p.CodeDiff(ctx, q)
			if !hasCode(err, "INVALID_INPUT") {
				t.Errorf("%s: err = %v, want INVALID_INPUT", name, err)
			}
		}
		_, err := p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: at.RepositoryID, Commit: "0123456789abcdef0123456789abcdef01234567"})
		wantCode(t, err, "COMMIT_NOT_FOUND")
		_, err = p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: at.RepositoryID, Base: "nope", Head: "main"})
		wantCode(t, err, "REF_NOT_FOUND")
		_, err = p.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: "missing", Commit: f.Root})
		wantCode(t, err, "REPOSITORY_NOT_FOUND")
		_, err = p.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: "missing"})
		wantCode(t, err, "WORKSPACE_NOT_FOUND")
	})

	t.Run("the architecture map counts files per context and zone", func(t *testing.T) {
		m, err := p.CodeMap(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		want := &domain.CodeMap{
			Source: domain.CodeSource{Kind: domain.CodeSourceRef, RepositoryID: at.RepositoryID, Ref: "main", Commit: f.Merge},
			Contexts: []domain.ContextCount{{ID: "ctx-core", Name: "Core", Purpose: "the core", FileCount: 1,
				Zones: []domain.ZoneCount{{ID: "zone-adapters", Name: "Adapters", Pattern: "^internal/", FileCount: 1}}}},
			UnassignedZones:  []domain.ZoneCount{{ID: "zone-web", Name: "Web", Pattern: "", FileCount: 1}},
			UnzonedFileCount: 17,
			FileCount:        19,
		}
		if !reflect.DeepEqual(m, want) {
			t.Fatalf("map = %s\nwant %s", show(m), show(want))
		}
		wm, err := p.CodeMap(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		if wm.Contexts[0].FileCount != 2 || wm.FileCount != 20 {
			t.Errorf("workspace map: core %d files, %d in all; want 2 and 20", wm.Contexts[0].FileCount, wm.FileCount)
		}
	})

	t.Run("a workspace whose worktree is gone", func(t *testing.T) {
		_, err := p.CodeTree(ctx, gone, "")
		wantCode(t, err, "WORKSPACE_MISSING")
		_, err = p.CodePaths(ctx, gone)
		wantCode(t, err, "WORKSPACE_MISSING")
		_, err = p.CodeFile(ctx, gone, "README.md")
		wantCode(t, err, "WORKSPACE_MISSING")
		_, err = p.CodeMap(ctx, gone)
		wantCode(t, err, "WORKSPACE_MISSING")
		for _, scope := range []string{"", "all", "working"} {
			_, err = p.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: at.GoneWorkspaceID, Scope: scope})
			wantCode(t, err, "WORKSPACE_MISSING")
		}
		d, err := p.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: at.GoneWorkspaceID, Scope: "branch"})
		if err != nil {
			t.Fatal(err)
		}
		id := at.GoneWorkspaceID
		want := domain.CodeSource{Kind: domain.CodeSourceWorkspace, RepositoryID: at.RepositoryID, WorkspaceID: &id, Ref: f.GoneBranch, Commit: f.GoneHead, Gone: true}
		if !reflect.DeepEqual(d.Source, want) || d.Base != f.Merge {
			t.Errorf("branch diff of a gone worktree: source %s base %s", show(d.Source), d.Base)
		}
		wantDiffPaths(t, d, []string{"gone.txt"})
	})

	t.Run("answers are JSON in the contract's shapes", func(t *testing.T) {
		tree, err := p.CodeTree(ctx, repo, "")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(tree.Entries[0])
		for _, key := range []string{`"size":null`, `"target":null`, `"change":null`, `"zones":[]`} {
			if !strings.Contains(string(raw), key) {
				t.Errorf("a dir entry %s lacks %s", raw, key)
			}
		}
		src, _ := json.Marshal(tree.Source)
		if !strings.Contains(string(src), `"workspace_id":null`) {
			t.Errorf("a ref source %s lacks workspace_id: null", src)
		}
	})
}

func kindOf(q ports.CodeSourceQuery) string {
	if q.WorkspaceID != "" {
		return "workspace"
	}
	return "ref"
}

func hasCode(err error, code string) bool { return errs.Code(err) == code }

func wantNames(t *testing.T, got []domain.TreeEntry, want []string) {
	t.Helper()
	names := make([]string, len(got))
	for i, e := range got {
		names[i] = e.Name
	}
	if !slices.Equal(names, want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
}

func wantDiffPaths(t *testing.T, d *domain.CodeDiff, want []string) {
	t.Helper()
	paths := make([]string, len(d.Files))
	for i, fd := range d.Files {
		paths[i] = fd.Path
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("diff files = %v, want %v", paths, want)
	}
}

func entries(list []domain.TreeEntry) map[string]domain.TreeEntry {
	m := map[string]domain.TreeEntry{}
	for _, e := range list {
		m[e.Name] = e
	}
	return m
}

func withheld(fd domain.FileDiff, why string) bool { return fd.Withheld != nil && *fd.Withheld == why }

func contentLen(f *domain.CodeFile) int {
	if f.Content == nil {
		return -1
	}
	return len(*f.Content)
}

func show(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(b)
}
