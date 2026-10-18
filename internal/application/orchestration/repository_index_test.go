//go:build !windows

package orchestration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/codegraph"
	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
)

// A session ending on a repository whose index is more than a day old
// rebuilds it in the background; one ending on a fresh index does not. The
// sessions end as the orchestration ends them, announcing SessionEnded on
// the bus the repository indexes follow. A rebuild that runs does not hold
// up a session starting.
func TestASessionEndingRebuildsAnIndexOlderThanADay(t *testing.T) {
	svc, _ := newInteractiveService(t)
	bus := eventbus.New()
	svc.Events = bus
	checkout := gitCheckout(t)
	stub := newStubCodegraph(t)
	indexes := workspaces.NewRepositoryIndexes(workspaces.RepositoryIndexDeps{
		Projects:      indexProjects{&domain.Repository{ID: "r1", ProjectID: "p1", Name: "api", RootDir: checkout}},
		Sessions:      svc,
		Installations: installedCodegraph(stub.path),
		Activations:   codegraphOnNowhere{},
		Indexer:       codegraph.NewIndexer(),
	})
	indexes.Subscribe(bus)
	t.Cleanup(indexes.Close)

	// The first build, as the Token savers page's Rebuild index starts it.
	if _, err := indexes.RebuildRepositoryIndex(context.Background(), "r1"); err != nil {
		t.Fatal(err)
	}
	indexes.Wait()
	stub.wantCalls(t, "init --yes "+checkout)

	endASession := func() {
		t.Helper()
		sess, _ := startInteractive(t, svc, InteractiveRequest{})
		if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
			t.Fatal(err)
		}
		indexes.Wait()
	}
	endASession()
	stub.wantCalls(t, "init --yes "+checkout)

	age(t, checkout, 25*time.Hour)
	endASession()
	stub.wantCalls(t, "init --yes "+checkout, "index --quiet "+checkout)

	// A rebuild held in codegraph does not delay the next session's start.
	age(t, checkout, 25*time.Hour)
	release := stub.hold(t)
	if _, err := indexes.RebuildRepositoryIndex(context.Background(), "r1"); err != nil {
		t.Fatal(err)
	}
	stub.waitCalls(t, 3)
	started := make(chan *domain.Session, 1)
	go func() {
		sess, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1"})
		if err != nil {
			t.Error(err)
		}
		started <- sess
	}()
	select {
	case sess := <-started:
		if sess == nil || sess.RepositoryID != "r1" {
			t.Errorf("session started without its repository: %+v", sess)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session start waited for the index build")
	}
	release()
	indexes.Wait()
}

// gitCheckout is a repository's checkout with one commit.
func gitCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "first"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// age sets the checkout's index back by d.
func age(t *testing.T, checkout string, d time.Duration) {
	t.Helper()
	then := time.Now().Add(-d)
	if err := os.Chtimes(filepath.Join(checkout, ".codegraph", "codegraph.db"), then, then); err != nil {
		t.Fatal(err)
	}
}

// stubCodegraph is an executable standing in for codegraph: it records each
// call's arguments, waits while its hold file exists, and writes an index
// into the directory it runs in.
type stubCodegraph struct{ path, log, holdFile string }

func newStubCodegraph(t *testing.T) *stubCodegraph {
	t.Helper()
	dir := t.TempDir()
	s := &stubCodegraph{path: filepath.Join(dir, "codegraph"), log: filepath.Join(dir, "calls.log"), holdFile: filepath.Join(dir, "hold")}
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> '" + s.log + "'\n" +
		"while [ -f '" + s.holdFile + "' ]; do sleep 0.02; done\n" +
		"mkdir -p .codegraph && echo index > .codegraph/codegraph.db\n"
	if err := os.WriteFile(s.path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *stubCodegraph) calls() []string {
	b, _ := os.ReadFile(s.log)
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func (s *stubCodegraph) wantCalls(t *testing.T, want ...string) {
	t.Helper()
	if got := s.calls(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("codegraph calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func (s *stubCodegraph) waitCalls(t *testing.T, n int) {
	t.Helper()
	eventually(t, "codegraph called", func() bool { return len(s.calls()) == n })
}

// hold keeps the next calls running until release.
func (s *stubCodegraph) hold(t *testing.T) (release func()) {
	t.Helper()
	if err := os.WriteFile(s.holdFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Remove(s.holdFile) }
}

type indexProjects struct{ repo *domain.Repository }

func (p indexProjects) GetProject(_ context.Context, id string) (*domain.Project, error) {
	return &domain.Project{ID: id, Name: "harness"}, nil
}

func (p indexProjects) ListProjects(context.Context) ([]*domain.Project, error) {
	return []*domain.Project{{ID: p.repo.ProjectID, Name: "harness"}}, nil
}

func (p indexProjects) ListRepositories(context.Context, string) ([]*domain.Repository, error) {
	return []*domain.Repository{p.repo}, nil
}

func (p indexProjects) GetRepository(_ context.Context, id string) (*domain.Repository, error) {
	if id != p.repo.ID {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	return p.repo, nil
}

type installedCodegraph string

func (c installedCodegraph) InstalledTokenSavers(context.Context) ([]*domain.TokenSaverInstallation, error) {
	return []*domain.TokenSaverInstallation{{Saver: domain.TokenSaverCodegraph, Version: "v1.6.2", Path: string(c)}}, nil
}

type codegraphOnNowhere struct{}

func (codegraphOnNowhere) RepositoriesWithTokenSaverOn(context.Context, domain.TokenSaver, string) ([]string, error) {
	return nil, nil
}
