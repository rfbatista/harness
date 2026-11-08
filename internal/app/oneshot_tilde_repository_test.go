package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/projects"
	"operators-mcp/internal/ports/portstest"
)

// A repository saved with a root dir typed as "~/…" once made every one-shot
// fail, because the sandbox policy listed its checkout as typed and the driver
// refuses a path that is not absolute (ADR 0072). The repository is now stored
// expanded, so the policy a one-shot is wrapped under names only absolute paths.
func TestOneShot_ARepositorySavedWithATildeRootDirNamesItsCheckoutByItsAbsolutePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projectStore := sqlite.NewProjectRepository(db)
	svc := projects.NewService(projectStore, sqlite.NewRepositoryRepository(db), nil)
	p, err := projectStore.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(context.Background(), p.ID, "harnesskit", "", "https://github.com/o/harnesskit", "~/projetos/harnesskit"); err != nil {
		t.Fatal(err)
	}

	driver := &portstest.RecordingSandboxDriver{}
	sb := &orchestration.Sandbox{
		Driver:    driver,
		Home:      t.TempDir(),
		Checkouts: func() []string { return allCheckouts(svc) },
	}
	wrap, err := sb.OneShotWrap(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrap(llmkit.Command{Path: "/opt/claude/bin/claude", Dir: os.TempDir()}); err != nil {
		t.Fatal(err)
	}

	calls := driver.Calls()
	if len(calls) != 1 {
		t.Fatalf("driver called %d times, want 1", len(calls))
	}
	pol := calls[0].Policy
	want := filepath.Join(home, "projetos/harnesskit")
	if !slices.Contains(pol.Readable, want) && !slices.Contains(pol.Hidden, want) {
		t.Errorf("policy = %+v, want the checkout %q read or hidden", pol, want)
	}
	for _, path := range slices.Concat(pol.Readable, pol.Writable, pol.CreateOnly, pol.Hidden) {
		if !filepath.IsAbs(path) {
			t.Errorf("policy names %q, which is not absolute", path)
		}
	}
}
