//go:build !windows

package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/capabilities"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
)

// TestRepositoryIndexBuiltWhenCodegraphTurnsOnInTheWebUI_EndToEnd: a person
// installs codegraph (from a fake release whose codegraph is a stub that
// records its calls) and turns it on for a project. The harness builds the
// index of the project's repository in its checkout, keeps .codegraph/ out
// of git through info/exclude (once), and the Token savers page shows the
// index and its age. Rebuild index runs codegraph there again.
func TestRepositoryIndexBuiltWhenCodegraphTurnsOnInTheWebUI_EndToEnd(t *testing.T) {
	w := newIndexWorld(t)
	assets := builtAssets(t)
	page := w.serve(assets)
	b := newTLSBrowser(t)

	b.run("install codegraph; no repository has it on yet",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(page+"/token-savers"),
		waitUntil(savers+`.length === 3`),
		clickButton(row("codegraph"), "Install"),
		waitUntil(stateOf("codegraph")+` === 'installed'`),
		waitUntil(indexesText+`.includes('No repository has codegraph on yet')`),
	)
	w.indexes.Wait()
	w.stub.wantCalls(t)

	b.run("turn codegraph on for the project",
		chromedp.Navigate(page+"/projects/"+w.project.ID+"/settings"),
		tokenSaversReady(),
		choose("codegraph", "on"),
		summaryIs("codegraph", "on — resolves on (project)"),
	)
	w.indexes.Wait()
	w.stub.wantCalls(t, "init --yes "+w.checkout)
	w.wantExcludedOnce()
	w.wantClean()

	b.run("the page lists the index and its age",
		chromedp.Navigate(page+"/token-savers"),
		waitUntil(indexRow(w.repo.ID)+`?.dataset.state === 'built'`),
		waitUntil(indexRowText(w.repo.ID)+`.includes('harness / api') && `+indexRowText(w.repo.ID)+`.includes('Built just now')`),
	)
	b.run("Rebuild index",
		clickButton(indexRow(w.repo.ID), "Rebuild index"),
		waitUntil(`(`+indexRow(w.repo.ID)+`?.dataset.state ?? '') === 'built' && !`+indexRow(w.repo.ID)+`.querySelector('button').disabled`),
	)
	w.indexes.Wait()
	w.stub.wantCalls(t, "init --yes "+w.checkout, "index --quiet "+w.checkout)
	w.wantExcludedOnce()
	w.wantClean()
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Turning codegraph on for a repository while it is not installed builds
// nothing; installing it then builds that repository's index.
func TestRepositoryIndexBuiltWhenCodegraphIsInstalledWhereItIsOn(t *testing.T) {
	w := newIndexWorld(t)
	repoOwner := domain.ActivationOwner{Level: domain.ActivationLevelRepository, ID: w.repo.ID}
	if _, err := w.cat.Capabilities.SetTokenSaverActivation(w.ctx, repoOwner, domain.TokenSaverCodegraph, domain.ActivationOn); err != nil {
		t.Fatal(err)
	}
	w.indexes.Wait()
	w.stub.wantCalls(t)

	if _, err := w.cat.Capabilities.InstallTokenSaver(w.ctx, domain.TokenSaverCodegraph); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); len(w.stub.calls()) == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("codegraph was installed, and no index was built")
		}
	}
	w.indexes.Wait()
	w.stub.wantCalls(t, "init --yes "+w.checkout)
	w.wantExcludedOnce()
	w.wantClean()
}

// indexWorld is the server as it runs, token savers included (over an
// in-memory database, codegraph from a fake release), with a project whose
// one repository is a git checkout.
type indexWorld struct {
	t        *testing.T
	ctx      context.Context
	cat      catalog.Catalog
	orch     *orchestration.Service
	plan     *planning.Service
	indexes  *workspaces.RepositoryIndexes
	stub     indexStub
	checkout string
	project  *domain.Project
	repo     *domain.Repository
}

func newIndexWorld(t *testing.T) *indexWorld {
	t.Helper()
	if testing.Short() {
		t.Skip("skipped in -short mode")
	}
	w := &indexWorld{t: t, ctx: context.Background(), checkout: gitCheckout(t)}
	w.stub = indexStub{log: filepath.Join(t.TempDir(), "codegraph-calls.log")}

	rel := newFakeReleases()
	rel.serveCodegraph(tarGz(map[string]string{
		"codegraph-darwin-arm64/node":          "#!/bin/sh\n",
		"codegraph-darwin-arm64/bin/codegraph": w.stub.script(),
	}, "codegraph-darwin-arm64/node", "codegraph-darwin-arm64/bin/codegraph"), true)
	releaseSrv := httptest.NewServer(rel)
	t.Cleanup(releaseSrv.Close)

	var db *gorm.DB
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: t.TempDir(), ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		app.TokenSaversModule,
		fx.Populate(&w.cat, &w.orch, &w.plan, &w.indexes, &db),
		fx.NopLogger,
	)
	if err := fxApp.Start(w.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })
	// The fake release serves darwin/arm64 only.
	w.cat.Capabilities.UseTokenSavers(capabilities.TokenSaverDeps{
		Installations:  sqlite.NewTokenSaverInstallationRepository(db),
		Downloads:      releases.NewClient(),
		Dir:            filepath.Join(t.TempDir(), "tools"),
		ReleaseBaseURL: releaseSrv.URL,
		Platform:       darwinARM64,
	})

	var err error
	if w.project, err = w.cat.Projects.CreateProject(w.ctx, "harness", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if w.repo, err = w.cat.Projects.CreateRepository(w.ctx, w.project.ID, "api", "", "file://"+w.checkout, w.checkout); err != nil {
		t.Fatal(err)
	}
	return w
}

// serve starts the pages and the API over HTTP/2: each page keeps an event
// stream open.
func (w *indexWorld) serve(assets *web.Assets) string {
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: w.cat.Projects, ProjectFeed: w.cat.Projects, Agents: w.cat.Agents, Sessions: w.orch, Planning: w.plan,
		TokenSavers: w.cat.Capabilities, TokenSaverInstalls: w.cat.Capabilities, RepositoryIndexes: w.indexes,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: w.cat.Projects, Tasks: w.plan, Sessions: w.orch, Agents: w.cat.Agents, Repositories: w.cat.Projects,
		EnvFiles: w.cat.Projects, ProjectSummaries: w.cat.Projects, TokenSavers: w.cat.Capabilities,
		TokenSaverInstalls: w.cat.Capabilities, RepositoryIndexes: w.indexes,
	}, assets, nil))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	w.t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv.URL
}

// wantExcludedOnce checks that the checkout's info/exclude names
// /.codegraph/ exactly once.
func (w *indexWorld) wantExcludedOnce() {
	w.t.Helper()
	b, err := os.ReadFile(filepath.Join(w.checkout, ".git", "info", "exclude"))
	if err != nil {
		w.t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "/.codegraph/" {
			n++
		}
	}
	if n != 1 {
		w.t.Errorf("info/exclude names /.codegraph/ %d times:\n%s", n, b)
	}
}

// wantClean checks that git sees no change in the checkout, though it holds
// an index.
func (w *indexWorld) wantClean() {
	w.t.Helper()
	if _, err := os.Stat(filepath.Join(w.checkout, ".codegraph", "codegraph.db")); err != nil {
		w.t.Fatalf("the checkout holds no index: %v", err)
	}
	out, err := exec.Command("git", "-C", w.checkout, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
	if err != nil {
		w.t.Fatalf("git status: %v\n%s", err, out)
	}
	if len(out) > 0 {
		w.t.Errorf("git status in the checkout:\n%s", out)
	}
}

// indexStub is a codegraph that records each call's arguments and writes an
// index into the directory it runs in.
type indexStub struct{ log string }

func (s indexStub) script() string {
	return "#!/bin/sh\necho \"$*\" >> '" + s.log + "'\nmkdir -p .codegraph && echo index > .codegraph/codegraph.db\n"
}

func (s indexStub) calls() []string {
	b, err := os.ReadFile(s.log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func (s indexStub) wantCalls(t *testing.T, want ...string) {
	t.Helper()
	if got := s.calls(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("codegraph calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// gitCheckout is a repository's checkout with one commit.
func gitCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "."},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "first"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

const indexesText = `(document.querySelector('section[aria-labelledby=repository-indexes-title]')?.innerText ?? '')`

// indexRow is the live row of the repository's index.
func indexRow(repositoryID string) string {
	return `[...document.querySelectorAll('main [data-repository-index="` + repositoryID + `"]')].find(r => !r.closest('[data-ssr]'))`
}

func indexRowText(repositoryID string) string {
	return `(` + indexRow(repositoryID) + `?.innerText ?? '')`
}
