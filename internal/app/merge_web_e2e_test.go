//go:build !windows

package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/domain"
)

// Merging a session's branch from the web UI, end to end: the server as it
// runs, real git, origin a local bare repository. One person merges from the
// session's Merge tab while another has the same tab open: the button is
// disabled while the merge runs (a hook holds it), the new HEAD shows on both
// without a reload and survives one; then the refusals are said in words, the
// checkout stays as it was, and no tab is offered to a session with no branch.
func TestMergingASessionsBranchInTheWebUI_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	f := startMergeFixture(t)
	f.session(t, "s-merge", map[string]string{"merge.txt": "merge\n"})
	f.session(t, "s-dirty", map[string]string{"dirty.txt": "dirty\n"})
	f.session(t, "s-conflict", map[string]string{"README.md": "from the agent\n"})
	if _, err := f.sess.Create(&domain.Session{ID: "s-nobranch", ProjectID: f.project.ID, RepositoryID: f.repo.ID, TicketID: f.task.ID, Task: "s-nobranch", Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: f.cat.Projects, Agents: f.cat.Agents, Sessions: f.orch, Planning: f.plan, Merges: f.merge,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: f.cat.Projects, Tasks: f.plan, Sessions: f.orch, Agents: f.cat.Agents, Repositories: f.cat.Projects,
		EnvFiles: f.cat.Projects, Documents: f.plan, History: f.ws,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the pages' event streams never end on their own
		srv.Close()
	})
	page := func(id string) string {
		return srv.URL + "/projects/" + f.project.ID + "/tasks/" + f.task.ID + "?session=" + id
	}
	const (
		live      = `document.querySelector('[x-data=streamStatus]')?.dataset.state === 'live'`
		mergeTab  = `[...document.querySelectorAll('[role=tab]')].find(b => b.textContent.trim() === 'Merge')`
		button    = `document.querySelector('[data-merge]')`
		result    = `document.querySelector('[data-merge-result]')`
		resultTxt = `(` + result + `?.innerText ?? '')`
	)
	open := func(p *person, step, id string) {
		p.run(t, step,
			chromedp.EmulateViewport(1280, 900),
			chromedp.Navigate(page(id)),
			waitFor(live+` && !!(`+mergeTab+`)`),
			chromedp.Evaluate(mergeTab+`.click()`, nil),
			waitFor(`!!`+button+` || !!document.querySelector('[data-merge-unavailable]')`),
		)
	}
	remote := f.remoteRefs(t)

	// A hook holds the merge long enough to see the button disabled.
	hook := filepath.Join(f.checkout, ".git", "hooks", "pre-merge-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nsleep 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	person, watcher := newPerson(t), newPerson(t)
	open(watcher, "the watcher opens the Merge tab", "s-merge")
	open(person, "the person opens the Merge tab", "s-merge")
	if got := person.text(t, button+`.textContent.trim() + ' ' + `+button+`.disabled`); got != "Merge into main false" {
		t.Fatalf("button = %q", got)
	}
	person.run(t, "the person merges",
		chromedp.Evaluate(button+`.click()`, nil),
		waitFor(button+`.disabled && `+button+`.getAttribute('aria-busy') === 'true'`),
		shot("merge-running", "light"),
		waitFor(`!!`+result),
	)
	head := gitE2E(t, f.checkout, "rev-parse", "HEAD")
	if got := person.text(t, resultTxt); !contains(got, "Merged into main", head[:8]) {
		t.Errorf("result = %q, want the new HEAD %s", got, head[:8])
	}
	if parents := strings.Fields(gitE2E(t, f.checkout, "rev-list", "--parents", "-n1", "HEAD")); len(parents) != 3 {
		t.Errorf("not a merge commit: %v", parents)
	}
	if gitE2E(t, f.checkout, "status", "--porcelain") != "" {
		t.Error("the checkout is not clean after the merge")
	}

	// The other tab gets the result from the stream, and a reload keeps it.
	watcher.run(t, "the watcher sees the merge without a reload", waitFor(`(`+resultTxt+`).includes('Merged into main')`))
	if got := watcher.text(t, resultTxt); !contains(got, head[:8]) {
		t.Errorf("watcher result = %q", got)
	}
	person.run(t, "after a reload", chromedp.Reload(), waitFor(live+` && !!(`+mergeTab+`)`), chromedp.Evaluate(mergeTab+`.click()`, nil), waitFor(`!!`+result))
	if got := person.text(t, resultTxt); !contains(got, "Merged into main", head[:8]) {
		t.Errorf("after the reload: %q", got)
	}

	// Merging again: refused, said in words, in the same place.
	person.run(t, "merge again",
		waitFor(`!!`+button),
		chromedp.Evaluate(button+`.click()`, nil),
		waitFor(`(`+resultTxt+`).includes('Not merged')`),
	)
	if got := person.text(t, resultTxt); !contains(got, "already merged", "MERGE_ALREADY_MERGED") {
		t.Errorf("merged twice: %q", got)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}

	// A dirty checkout, then another branch checked out: each refusal says what is in the way.
	if err := os.WriteFile(filepath.Join(f.checkout, "notes.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot(t)
	open(person, "the dirty session", "s-dirty")
	person.run(t, "merge into a dirty checkout",
		chromedp.Evaluate(button+`.click()`, nil),
		waitFor(`(`+resultTxt+`).includes('Not merged')`),
		waitFor(`!`+button+`.disabled`),
		shot("merge-refused", "light"),
	)
	if got := person.text(t, resultTxt); !contains(got, "untracked files (notes.txt)", "MERGE_CHECKOUT_DIRTY") {
		t.Errorf("dirty: %q", got)
	}
	if f.snapshot(t) != snap {
		t.Error("a refusal changed the dirty checkout")
	}
	if err := os.Remove(filepath.Join(f.checkout, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, f.checkout, "checkout", "-q", "-b", "elsewhere")
	person.run(t, "merge from another branch",
		chromedp.Evaluate(button+`.click()`, nil),
		waitFor(`(`+resultTxt+`).includes('MERGE_WRONG_BRANCH')`),
	)
	if got := person.text(t, resultTxt); !contains(got, "is on elsewhere, not on the default branch main") {
		t.Errorf("wrong branch: %q", got)
	}
	gitE2E(t, f.checkout, "checkout", "-q", "main")

	// A conflict: aborted, the checkout exactly as it was.
	if err := os.WriteFile(filepath.Join(f.checkout, "README.md"), []byte("from main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, f.checkout, "commit", "-q", "-am", "main edits README")
	snap = f.snapshot(t)
	open(person, "the conflicting session", "s-conflict")
	person.run(t, "merge a conflict",
		chromedp.Evaluate(button+`.click()`, nil),
		waitFor(`(`+resultTxt+`).includes('MERGE_CONFLICT')`),
	)
	if got := person.text(t, resultTxt); !contains(got, "conflicts in README.md", "the checkout is as it was") {
		t.Errorf("conflict: %q", got)
	}
	if got := f.snapshot(t); got != snap {
		t.Errorf("after the conflict the checkout changed:\nbefore %s\nafter  %s", snap, got)
	}

	// No branch, no Merge tab.
	person.run(t, "a session without a branch",
		chromedp.Navigate(page("s-nobranch")),
		waitFor(live+` && !!document.querySelector('[role=tab]')`),
	)
	if got := person.truth(t, `!(`+mergeTab+`)`); !got {
		t.Error("a session without a branch has a Merge tab")
	}

	if got := f.remoteRefs(t); got != remote {
		t.Errorf("origin changed:\nbefore %s\nafter  %s", remote, got)
	}
}
