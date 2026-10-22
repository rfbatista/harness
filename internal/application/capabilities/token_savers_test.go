package capabilities

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// Closing the token savers, as the server stops, cancels an install's
// download and waits for the install to end; none starts afterwards.
func TestClosingTokenSaversCancelsAndWaitsForTheirInstalls(t *testing.T) {
	downloads := &blockingDownloads{started: make(chan struct{}, 1)}
	installs := &memoryInstallations{}
	s := NewService(Deps{})
	s.UseTokenSavers(TokenSaverDeps{
		Installations: installs, Downloads: downloads, Dir: t.TempDir(),
		Platform: domain.Platform{OS: "darwin", Arch: "arm64"},
	})
	if _, err := s.InstallTokenSaver(context.Background(), domain.TokenSaverRTK); err != nil {
		t.Fatal(err)
	}
	select {
	case <-downloads.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the install never started downloading")
	}

	closed := make(chan struct{})
	go func() {
		s.CloseTokenSavers()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("closing did not cancel the running download")
	}
	if !downloads.cancelled {
		t.Error("the download returned without its context cancelled")
	}
	if len(installs.list) != 0 {
		t.Errorf("a cancelled install recorded %+v", installs.list)
	}
	if _, err := s.InstallTokenSaver(context.Background(), domain.TokenSaverRTK); err == nil {
		t.Error("an install started after closing")
	}
}

// blockingDownloads holds every download until its context ends.
type blockingDownloads struct {
	started   chan struct{}
	cancelled bool
}

func (d *blockingDownloads) Download(ctx context.Context, _ string, _ io.Writer, _ func(done, total int64)) error {
	d.started <- struct{}{}
	<-ctx.Done()
	d.cancelled = true
	return ctx.Err()
}

type memoryInstallations struct {
	list []*domain.TokenSaverInstallation
}

func (m *memoryInstallations) List() ([]*domain.TokenSaverInstallation, error) { return m.list, nil }

func (m *memoryInstallations) Put(in *domain.TokenSaverInstallation) error {
	for i, old := range m.list {
		if old.Saver == in.Saver {
			m.list[i] = in
			return nil
		}
	}
	m.list = append(m.list, in)
	return nil
}

// An installation recorded with relative paths, as a server started with a
// relative database path once stored it, is made absolute when the token
// savers start: hooks, MCP servers and index builds run from other
// directories.
func TestStartingTokenSaversMakesRelativeInstallationPathsAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	installs := &memoryInstallations{list: []*domain.TokenSaverInstallation{{
		Saver: domain.TokenSaverCodegraph, Version: "v1.6.2",
		Dir:  "tools/codegraph/v1.6.2",
		Path: "tools/codegraph/v1.6.2/codegraph-darwin-arm64/bin/codegraph",
	}}}
	s := NewService(Deps{})
	s.UseTokenSavers(TokenSaverDeps{Installations: installs, Downloads: &blockingDownloads{}, Dir: t.TempDir()})
	defer s.CloseTokenSavers()

	in := installs.list[0]
	wantDir, _ := filepath.Abs("tools/codegraph/v1.6.2")
	wantPath, _ := filepath.Abs("tools/codegraph/v1.6.2/codegraph-darwin-arm64/bin/codegraph")
	if in.Dir != wantDir || in.Path != wantPath {
		t.Errorf("installation = %q, %q; want %q, %q", in.Dir, in.Path, wantDir, wantPath)
	}
}

// A person moved the data dir: an installation recorded at the old place
// points at the new one when its version is there, and one that is not
// there is left as recorded.
func TestStartingTokenSaversRelocatesInstallationsOfAMovedDataDir(t *testing.T) {
	tools := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tools, "rtk", "v0.30.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	installs := &memoryInstallations{list: []*domain.TokenSaverInstallation{
		{Saver: domain.TokenSaverRTK, Version: "v0.30.1", Dir: "/old/data/tools/rtk/v0.30.1", Path: "/old/data/tools/rtk/v0.30.1/rtk"},
		{Saver: domain.TokenSaverCodegraph, Version: "v1.6.2", Dir: "/old/data/tools/codegraph/v1.6.2", Path: "/old/data/tools/codegraph/v1.6.2/bin/codegraph"},
	}}
	s := NewService(Deps{})
	s.UseTokenSavers(TokenSaverDeps{Installations: installs, Downloads: &blockingDownloads{}, Dir: tools})
	defer s.CloseTokenSavers()

	rtk, codegraph := installs.list[0], installs.list[1]
	if want := filepath.Join(tools, "rtk", "v0.30.1"); rtk.Dir != want || rtk.Path != filepath.Join(want, "rtk") {
		t.Errorf("rtk = %q, %q; want %q, %q", rtk.Dir, rtk.Path, want, filepath.Join(want, "rtk"))
	}
	if codegraph.Dir != "/old/data/tools/codegraph/v1.6.2" {
		t.Errorf("codegraph moved to %q, though its version is not in the new data dir", codegraph.Dir)
	}
}

func (m *memoryInstallations) Delete(domain.TokenSaver) error { return nil }
